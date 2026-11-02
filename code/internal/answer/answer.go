// Package answer coordinates answer generation for one question:
//
//  1. Send the complete reference material and the question to the LLM and
//     ask for a document-only answer with verbatim supporting quotes, plus an
//     assessment of whether the documents are sufficient.
//  2. Verify every citation against the extracted documents.
//  3. Only if the documents are insufficient, search externally for the
//     missing information and ask the LLM to fill just the gaps from the
//     actual search results.
//  4. Derive the answer status from the verified sources, not from what the
//     model claims.
package answer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"revisiontool/internal/llm"
	"revisiontool/internal/search"
	"revisiontool/internal/store"
)

// Service generates answers. It holds no per-session state.
type Service struct {
	LLM              llm.Client
	Search           search.Provider
	ContextWindow    int
	MaxOutputTokens  int
	SearchMaxResults int
}

const documentSystemPrompt = `You are an examination revision assistant. You answer exam questions for a student using ONLY the student's uploaded reference documents, which are the source of truth.

Rules:
- Use only information stated in the reference documents. Do not add facts from your own knowledge, even if you are sure they are correct.
- Split your answer into segments. Every segment must be supported by at least one citation:
  - "document_id": the document id, e.g. "D1"
  - "page": the page number from <page number="...">
  - "section": the nearest heading (lines starting with "## ") above the cited text, or ""
  - "quote": a short passage (5 to 30 words) copied VERBATIM, character for character, from that page, that supports the segment
- Never invent or guess citations, page numbers or quotes. If you cannot quote support for a statement, leave that statement out.
- "coverage": "complete" if the documents fully answer the question at the depth its wording and marks require; "partial" if they answer only part of it; "none" if they contain nothing relevant (then return no segments).
- If coverage is "partial" or "none": in "missing_information" state precisely what the documents do not cover, and in "search_queries" give 1 to 3 concise web search queries for ONLY that missing information. Otherwise use "" and [].
- If the documents contradict each other on this question, describe the contradiction in "contradictions" (otherwise "").
- If the question or the material is ambiguous, explain the ambiguity in "ambiguity" (otherwise "").
- Write clear, educational explanations that help the student understand the concept, not just recall it. Plain text; you may use lines starting with "- " for lists.`

var documentSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"coverage": map[string]any{"type": "string", "enum": []string{"complete", "partial", "none"}},
		"segments": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
					"citations": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"document_id": map[string]any{"type": "string"},
								"page":        map[string]any{"type": "integer"},
								"section":     map[string]any{"type": "string"},
								"quote":       map[string]any{"type": "string"},
							},
							"required":             []string{"document_id", "page", "section", "quote"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"text", "citations"},
				"additionalProperties": false,
			},
		},
		"missing_information": map[string]any{"type": "string"},
		"search_queries":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"contradictions":      map[string]any{"type": "string"},
		"ambiguity":           map[string]any{"type": "string"},
	},
	"required":             []string{"coverage", "segments", "missing_information", "search_queries", "contradictions", "ambiguity"},
	"additionalProperties": false,
}

type documentReply struct {
	Coverage string `json:"coverage"`
	Segments []struct {
		Text      string     `json:"text"`
		Citations []citation `json:"citations"`
	} `json:"segments"`
	MissingInformation string   `json:"missing_information"`
	SearchQueries      []string `json:"search_queries"`
	Contradictions     string   `json:"contradictions"`
	Ambiguity          string   `json:"ambiguity"`
}

const externalSystemPrompt = `You complete a student's exam answer using web search results. Part of the answer may already have been written from the student's own reference documents; those documents are authoritative.

Rules:
- Write segments that cover ONLY the missing information described. Do not repeat or rewrite the document-based part.
- Use only facts stated in the provided search results, and list the ids of the results ("E1", "E2", ...) that support each segment in "source_ids". Never cite anything that is not in the list.
- Do not contradict the document-based part. If the search results disagree with it, do not use them for that point and describe the disagreement in "conflicts".
- Prefer results from educational institutions, textbooks and reputable publications.
- If the results do not reliably answer the missing part, return no segments and set "insufficient" to true. Do not fill gaps from your own knowledge.
- Write clear, educational explanations. Plain text; you may use lines starting with "- " for lists.`

var externalSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"segments": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":       map[string]any{"type": "string"},
					"source_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required":             []string{"text", "source_ids"},
				"additionalProperties": false,
			},
		},
		"insufficient": map[string]any{"type": "boolean"},
		"conflicts":    map[string]any{"type": "string"},
	},
	"required":             []string{"segments", "insufficient", "conflicts"},
	"additionalProperties": false,
}

type externalReply struct {
	Segments []struct {
		Text      string   `json:"text"`
		SourceIDs []string `json:"source_ids"`
	} `json:"segments"`
	Insufficient bool   `json:"insufficient"`
	Conflicts    string `json:"conflicts"`
}

// Generate answers question q. chain holds q's ancestors (outermost first)
// so sub-questions are answered with their shared stem.
func (s *Service) Generate(ctx context.Context, ref *Reference, chain []*store.Question, q *store.Question) (*store.Answer, error) {
	if err := CheckBudget(ref.Tokens(), s.ContextWindow, s.MaxOutputTokens); err != nil {
		return nil, err
	}
	questionText := formatQuestion(chain, q)

	// Step 1: documents only.
	out, err := s.LLM.Complete(ctx, llm.Request{
		System:    documentSystemPrompt,
		Reference: ref.Text,
		Prompt:    questionText + "\n\nAnswer this question from the reference documents above, following the rules.",
		Schema:    documentSchema,
		MaxTokens: s.MaxOutputTokens,
	})
	if err != nil {
		return nil, err
	}
	var dr documentReply
	if err := llm.DecodeJSON(out, &dr); err != nil {
		return nil, err
	}

	ans := &store.Answer{
		QuestionID:     q.ID,
		Model:          s.LLM.Model(),
		MissingInfo:    strings.TrimSpace(dr.MissingInformation),
		Contradictions: strings.TrimSpace(dr.Contradictions),
		Ambiguity:      strings.TrimSpace(dr.Ambiguity),
	}
	var notices []string

	// Step 2: verify document citations.
	unverified := 0
	for _, seg := range dr.Segments {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}
		out := store.Segment{Text: text, Basis: store.BasisUnsupported}
		for _, c := range seg.Citations {
			src, ok := ref.verifyCitation(c)
			if !ok {
				continue // cited a document that does not exist: discard
			}
			out.Sources = append(out.Sources, src)
			if src.Verified {
				out.Basis = store.BasisDocument
			}
		}
		if out.Basis == store.BasisUnsupported {
			unverified++
		}
		ans.Segments = append(ans.Segments, out)
	}
	if unverified > 0 {
		notices = append(notices, fmt.Sprintf("%d part(s) of the answer could not be matched to the cited passages in your documents and are marked as unverified. Treat them with caution.", unverified))
	}

	// Step 3: external search, only when the documents are insufficient.
	coverage := strings.ToLower(strings.TrimSpace(dr.Coverage))
	needExternal := coverage != "complete" || unverified > 0 || !hasBasis(ans.Segments, store.BasisDocument)
	if needExternal {
		if ans.MissingInfo == "" && coverage == "complete" {
			ans.MissingInfo = "Parts of the answer whose document citations could not be verified."
		}
		notices = append(notices, s.supplement(ctx, ans, questionText, dr.SearchQueries)...)
	}

	// Step 4: status from verified sources.
	ans.Status = statusOf(ans.Segments)
	if ans.Status == store.StatusUnanswered {
		notices = append([]string{"No reliable answer could be established from your documents or from external sources."}, notices...)
	}
	ans.Notice = strings.Join(notices, "\n")
	return ans, nil
}

// supplement runs external search for the missing information and appends
// externally supported segments. It returns notices for the student.
func (s *Service) supplement(ctx context.Context, ans *store.Answer, questionText string, queries []string) []string {
	notInDocs := "The uploaded documents do not fully answer this question."
	if !hasBasis(ans.Segments, store.BasisDocument) {
		notInDocs = "This answer was not found in the uploaded documents."
	}
	if s.Search == nil || !s.Search.Available() {
		return []string{notInDocs + " External search is not configured, so no external sources were consulted (see README to enable it)."}
	}

	queries = cleanQueries(queries)
	if len(queries) == 0 {
		queries = []string{truncateWords(questionText, 30)}
	}
	var results []search.Result
	seen := map[string]bool{}
	var searchErrs []string
	for _, query := range queries {
		res, err := s.Search.Search(ctx, query, s.SearchMaxResults)
		if err != nil {
			if errors.Is(err, search.ErrUnavailable) {
				return []string{notInDocs + " External search is not available."}
			}
			searchErrs = append(searchErrs, err.Error())
			continue
		}
		for _, r := range res {
			if !seen[r.URL] {
				seen[r.URL] = true
				results = append(results, r)
			}
		}
	}
	if len(results) == 0 {
		if len(searchErrs) > 0 {
			return []string{notInDocs + " External search failed: " + searchErrs[0]}
		}
		return []string{notInDocs + " External search returned no results."}
	}

	var b strings.Builder
	b.WriteString(questionText)
	b.WriteString("\n\nDocument-based part of the answer (already written, authoritative):\n")
	wrote := false
	for _, seg := range ans.Segments {
		if seg.Basis == store.BasisDocument {
			b.WriteString(seg.Text + "\n")
			wrote = true
		}
	}
	if !wrote {
		b.WriteString("(none - the documents did not answer this question)\n")
	}
	fmt.Fprintf(&b, "\nMissing information to cover: %s\n\nSearch results:\n", orDefault(ans.MissingInfo, "the whole question"))
	byID := map[string]search.Result{}
	for i, r := range results {
		id := fmt.Sprintf("E%d", i+1)
		byID[id] = r
		fmt.Fprintf(&b, "<result id=%q title=%q url=%q>\n%s\n</result>\n", id, r.Title, r.URL, r.Content)
	}

	out, err := s.LLM.Complete(ctx, llm.Request{
		System:    externalSystemPrompt,
		Prompt:    b.String(),
		Schema:    externalSchema,
		MaxTokens: s.MaxOutputTokens,
	})
	var er externalReply
	if err == nil {
		err = llm.DecodeJSON(out, &er)
	}
	if err != nil {
		return []string{notInDocs + " Search results were found but could not be processed: " + err.Error()}
	}

	var notices []string
	added := 0
	for _, seg := range er.Segments {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}
		out := store.Segment{Text: text, Basis: store.BasisUnsupported}
		used := map[string]bool{}
		for _, id := range seg.SourceIDs {
			id = strings.TrimSpace(id)
			r, ok := byID[id]
			if !ok || used[id] {
				continue // only real search results can be cited
			}
			used[id] = true
			out.Sources = append(out.Sources, store.Source{Kind: "external", Title: r.Title, URL: r.URL, Snippet: truncateWords(r.Content, 60)})
			out.Basis = store.BasisExternal
		}
		if out.Basis == store.BasisExternal {
			added++
		} else {
			notices = append(notices, "An externally sourced part of the answer cited no valid search result and is marked as unverified.")
		}
		ans.Segments = append(ans.Segments, out)
	}
	if c := strings.TrimSpace(er.Conflicts); c != "" {
		if ans.Contradictions != "" {
			ans.Contradictions += "\n"
		}
		ans.Contradictions += "External sources vs. your documents: " + c
	}
	if added == 0 {
		notices = append(notices, notInDocs+" External sources were searched but did not provide a reliable answer for the missing information.")
	} else {
		notices = append(notices, notInDocs+" Parts marked “External” come from web sources, not from your documents.")
	}
	return notices
}

func statusOf(segs []store.Segment) string {
	doc, ext := hasBasis(segs, store.BasisDocument), hasBasis(segs, store.BasisExternal)
	switch {
	case doc && ext:
		return store.StatusSupplemented
	case doc:
		return store.StatusDocument
	case ext:
		return store.StatusExternal
	}
	return store.StatusUnanswered
}

func hasBasis(segs []store.Segment, basis string) bool {
	for _, s := range segs {
		if s.Basis == basis {
			return true
		}
	}
	return false
}

func formatQuestion(chain []*store.Question, q *store.Question) string {
	var b strings.Builder
	for _, p := range chain {
		if p.Text != "" {
			fmt.Fprintf(&b, "Context from question %s: %s\n", p.Number, p.Text)
		}
	}
	fmt.Fprintf(&b, "Question %s: %s", q.Number, q.Text)
	if q.Marks != "" {
		fmt.Fprintf(&b, " (%s)", q.Marks)
	}
	return b.String()
}

func cleanQueries(qs []string) []string {
	var out []string
	for _, q := range qs {
		if q = strings.TrimSpace(q); q != "" && len(out) < 3 {
			out = append(out, q)
		}
	}
	return out
}

func truncateWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) <= n {
		return strings.Join(f, " ")
	}
	return strings.Join(f[:n], " ") + "..."
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
