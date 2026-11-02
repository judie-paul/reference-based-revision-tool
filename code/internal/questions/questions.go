// Package questions extracts individual questions from a question paper.
//
// Exam papers vary too much for fixed patterns, so the LLM identifies the
// questions and their hierarchy. Its output is not trusted blindly: every
// extracted question is checked against the paper's real text, and any paper
// text that no question accounts for is reported so omissions are visible.
package questions

import (
	"context"
	"fmt"
	"strings"

	"revisiontool/internal/extract"
	"revisiontool/internal/llm"
	"revisiontool/internal/store"
)

const systemPrompt = `You split examination question papers into individual questions.
Rules:
- Return every question and sub-question in the order they appear in the paper.
- "number" is the full label as a student would cite it, e.g. "1", "1(a)", "1(a)(ii)", "Section B 3".
- "parent_number" is the number of the enclosing question ("1" for "1(a)", "1(a)" for "1(a)(ii)"), or "" for top-level questions.
- If a question has a shared stem/introduction followed by sub-questions, return the stem as the parent question's text and each sub-question separately.
- If a parent question has no text of its own, return it with text "".
- Copy question text VERBATIM from the paper (without the number label). Do not paraphrase, fix, summarise or merge questions.
- "marks" is the mark allocation if printed (e.g. "4 marks"), otherwise "".
- Do not include general instructions, headers, or rubrics as questions.
- In "problems", describe any part of the paper you could not split reliably (garbled text, missing numbering, unreadable sections). Use [] if none.`

var schema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"questions": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"number":        map[string]any{"type": "string"},
					"parent_number": map[string]any{"type": "string"},
					"text":          map[string]any{"type": "string"},
					"marks":         map[string]any{"type": "string"},
				},
				"required":             []string{"number", "parent_number", "text", "marks"},
				"additionalProperties": false,
			},
		},
		"problems": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
	"required":             []string{"questions", "problems"},
	"additionalProperties": false,
}

type reply struct {
	Questions []struct {
		Number       string `json:"number"`
		ParentNumber string `json:"parent_number"`
		Text         string `json:"text"`
		Marks        string `json:"marks"`
	} `json:"questions"`
	Problems []string `json:"problems"`
}

// Result is the outcome of extraction.
type Result struct {
	Questions []store.Question
	// Notes lists problems the user should know about: text not assigned to
	// any question and problems reported by the model.
	Notes []string
}

// Extract identifies the questions in the paper. contextWindow and
// maxOutput are the model's limits, used to refuse papers that do not fit.
func Extract(ctx context.Context, client llm.Client, paper *extract.Document, contextWindow, maxOutput int) (*Result, error) {
	text := paperText(paper)
	if need := llm.EstimateTokens(systemPrompt+text) + maxOutput; need > contextWindow {
		return nil, fmt.Errorf("the question paper is too long for the model's context window (about %d tokens needed, %d available)", need, contextWindow)
	}
	out, err := client.Complete(ctx, llm.Request{
		System:    systemPrompt,
		Prompt:    "Question paper:\n\n" + text,
		Schema:    schema,
		MaxTokens: maxOutput,
	})
	if err != nil {
		return nil, err
	}
	var r reply
	if err := llm.DecodeJSON(out, &r); err != nil {
		return nil, err
	}
	return build(r, paper), nil
}

func paperText(d *extract.Document) string {
	var b strings.Builder
	for _, p := range d.Pages {
		fmt.Fprintf(&b, "[Page %d]\n%s\n\n", p.Number, p.PageText())
	}
	return b.String()
}

// build converts the model reply into store questions, verifying each one
// against the paper text and collecting uncovered paper text.
func build(r reply, paper *extract.Document) *Result {
	cov := newCoverage(paper.PlainText())
	res := &Result{}
	lastIndex := map[string]int{}
	for _, q := range r.Questions {
		number := strings.TrimSpace(q.Number)
		text := strings.TrimSpace(q.Text)
		if number == "" && text == "" {
			continue
		}
		if number == "" {
			number = "?"
		}
		parent := -1
		if pn := strings.TrimSpace(q.ParentNumber); pn != "" {
			if i, ok := lastIndex[pn]; ok {
				parent = i
			}
		}
		verified := text == "" || cov.match(text)
		res.Questions = append(res.Questions, store.Question{
			Number:    number,
			ParentSeq: parent,
			Text:      text,
			Marks:     strings.TrimSpace(q.Marks),
			Verified:  verified,
		})
		lastIndex[number] = len(res.Questions) - 1
	}
	for _, p := range r.Problems {
		if p = strings.TrimSpace(p); p != "" {
			res.Notes = append(res.Notes, "Extraction problem reported: "+p)
		}
	}
	for _, span := range cov.uncovered(8) {
		res.Notes = append(res.Notes, "Paper text not assigned to any question: “"+span+"”")
	}
	return res
}

// coverage tracks which words of the paper are accounted for by questions,
// using 4-word shingles so small differences (line breaks, punctuation,
// a dropped label) do not break matching.
type coverage struct {
	orig    []string
	norm    []string
	covered []bool
	grams   map[string][]int
}

const gram = 4

func tokenize(s string) (orig, norm []string) {
	for _, tok := range strings.Fields(s) {
		n := strings.ReplaceAll(extract.Normalize(tok), " ", "")
		if n == "" {
			continue
		}
		orig = append(orig, tok)
		norm = append(norm, n)
	}
	return
}

func newCoverage(paper string) *coverage {
	c := &coverage{grams: map[string][]int{}}
	c.orig, c.norm = tokenize(paper)
	c.covered = make([]bool, len(c.norm))
	for i := 0; i+gram <= len(c.norm); i++ {
		k := strings.Join(c.norm[i:i+gram], " ")
		c.grams[k] = append(c.grams[k], i)
	}
	return c
}

// match reports whether text appears in the paper (allowing small
// differences) and marks the matching words as covered.
func (c *coverage) match(text string) bool {
	_, qn := tokenize(text)
	if len(qn) == 0 {
		return true
	}
	if len(qn) < gram {
		// Too short for shingles: require an exact word-sequence match.
		joined := " " + strings.Join(c.norm, " ") + " "
		return strings.Contains(joined, " "+strings.Join(qn, " ")+" ")
	}
	total, hits := 0, 0
	for i := 0; i+gram <= len(qn); i++ {
		total++
		if pos, ok := c.grams[strings.Join(qn[i:i+gram], " ")]; ok {
			hits++
			for _, p := range pos {
				for j := p; j < p+gram; j++ {
					c.covered[j] = true
				}
			}
		}
	}
	return float64(hits) >= 0.8*float64(total)
}

// uncovered returns runs of at least minWords consecutive words that no
// question matched.
func (c *coverage) uncovered(minWords int) []string {
	var out []string
	start := -1
	flush := func(end int) {
		if start >= 0 && end-start >= minWords {
			s := strings.Join(c.orig[start:end], " ")
			if len(s) > 400 {
				s = s[:400] + "..."
			}
			out = append(out, s)
		}
		start = -1
	}
	for i, cv := range c.covered {
		if !cv && start < 0 {
			start = i
		} else if cv {
			flush(i)
		}
	}
	flush(len(c.covered))
	return out
}
