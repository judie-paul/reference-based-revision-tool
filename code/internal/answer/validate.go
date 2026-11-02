package answer

import (
	"strings"

	"revisiontool/internal/extract"
	"revisiontool/internal/store"
)

// citation is a document citation as claimed by the model.
type citation struct {
	DocumentID string `json:"document_id"`
	Page       int    `json:"page"`
	Section    string `json:"section"`
	Quote      string `json:"quote"`
}

// minQuoteWords is the shortest quote accepted as evidence; very short
// quotes match almost anywhere and prove nothing.
const minQuoteWords = 3

// verifyCitation checks a model-claimed citation against the extracted
// document. The page and section stored come from where the quote was
// actually found, never from the model's claim alone. ok is false when the
// citation names a document that does not exist.
func (r *Reference) verifyCitation(c citation) (src store.Source, ok bool) {
	doc, exists := r.docs[strings.TrimSpace(c.DocumentID)]
	if !exists {
		return store.Source{}, false
	}
	src = store.Source{
		Kind:       "document",
		DocumentID: doc.ID,
		Filename:   doc.Filename,
		Quote:      strings.TrimSpace(c.Quote),
		PageApprox: !doc.Content.PageNumbersReliable,
	}

	q := extract.Normalize(c.Quote)
	if len(strings.Fields(q)) >= minQuoteWords {
		// Look on the claimed page first, then the rest of the document.
		order := []int{c.Page}
		for _, p := range doc.Content.Pages {
			if p.Number != c.Page {
				order = append(order, p.Number)
			}
		}
		for _, n := range order {
			if page, section, found := findQuote(doc.Content, n, q); found {
				src.Page, src.Section, src.Verified = page, section, true
				return src, true
			}
		}
	}

	// Not verified: keep the claimed page only if it exists in the document.
	if doc.Content.FindPage(c.Page) != nil {
		src.Page = c.Page
	}
	return src, true
}

// findQuote looks for a normalized quote on page n (also allowing it to run
// onto the next page). It returns the page where the quote starts and the
// section heading in effect there.
func findQuote(doc *extract.Document, n int, q string) (int, string, bool) {
	page := doc.FindPage(n)
	if page == nil {
		return 0, "", false
	}
	text := extract.Normalize(page.PageText())
	if !strings.Contains(text, q) {
		next := doc.FindPage(n + 1)
		if next == nil || !strings.Contains(text+" "+extract.Normalize(next.PageText()), q) {
			return 0, "", false
		}
	}
	// Section: the paragraph containing the start of the quote.
	head := strings.Join(firstWords(q, minQuoteWords), " ")
	section := ""
	for _, para := range page.Paragraphs {
		if strings.Contains(extract.Normalize(para.Text), head) {
			section = para.Section
			break
		}
	}
	if section == "" && len(page.Paragraphs) > 0 {
		section = page.Paragraphs[0].Section
	}
	return n, section, true
}

func firstWords(s string, n int) []string {
	f := strings.Fields(s)
	if len(f) > n {
		f = f[:n]
	}
	return f
}
