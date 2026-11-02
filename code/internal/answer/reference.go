package answer

import (
	"fmt"
	"strings"

	"revisiontool/internal/llm"
	"revisiontool/internal/store"
)

// Reference is the full reference material of a session, rendered once and
// sent unchanged with every question (which also lets providers cache it).
type Reference struct {
	Text string
	// docs maps the label shown to the model ("D1") to the stored document.
	docs  map[string]*store.Document
	order []string
}

// BuildReference renders all reference documents, in full, with document
// labels, page numbers and section headings the model must cite.
func BuildReference(docs []*store.Document) *Reference {
	ref := &Reference{docs: map[string]*store.Document{}}
	var b strings.Builder
	b.WriteString("<reference_documents>\n")
	for i, d := range docs {
		label := fmt.Sprintf("D%d", i+1)
		ref.docs[label] = d
		ref.order = append(ref.order, label)
		pages := "exact"
		if !d.Content.PageNumbersReliable {
			pages = "approximate"
		}
		fmt.Fprintf(&b, "<document id=%q filename=%q page_numbers=%q>\n", label, d.Filename, pages)
		for _, p := range d.Content.Pages {
			fmt.Fprintf(&b, "<page number=\"%d\">\n", p.Number)
			for _, para := range p.Paragraphs {
				if para.IsHeading {
					b.WriteString("## ")
				}
				b.WriteString(para.Text)
				b.WriteString("\n\n")
			}
			b.WriteString("</page>\n")
		}
		b.WriteString("</document>\n")
	}
	b.WriteString("</reference_documents>")
	ref.Text = b.String()
	return ref
}

// Tokens estimates the token size of the rendered reference material.
func (r *Reference) Tokens() int { return llm.EstimateTokens(r.Text) }

// Fixed allowances, in tokens, for the parts of a request other than the
// reference material: instructions, the question and the reply.
const (
	instructionAllowance = 2_000
	questionAllowance    = 2_000
)

// BudgetError explains that the documents do not fit the model's context.
type BudgetError struct {
	Needed, Available int
}

func (e *BudgetError) Error() string {
	return fmt.Sprintf("the uploaded reference documents are too large for the configured model: "+
		"about %d tokens are needed but the context budget is %d tokens. "+
		"Nothing was truncated or left out - please upload fewer or smaller documents, "+
		"or configure a model with a larger context window", e.Needed, e.Available)
}

// CheckBudget verifies that the complete reference text, instructions,
// question and the expected answer all fit in the context window.
func CheckBudget(referenceTokens, contextWindow, maxOutput int) error {
	need := referenceTokens + instructionAllowance + questionAllowance + maxOutput
	if need > contextWindow {
		return &BudgetError{Needed: need, Available: contextWindow}
	}
	return nil
}
