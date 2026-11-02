// Package extract turns uploaded PDF and DOCX files into structured text:
// pages made of paragraphs, each paragraph tagged with the section heading it
// belongs to. This structure is what source attribution is validated against.
package extract

import (
	"fmt"
	"strings"
	"unicode"

	"revisiontool/internal/upload"
)

// Document is the extracted content of one uploaded file.
type Document struct {
	Pages []Page `json:"pages"`
	// PageNumbersReliable is true when page numbers reflect the real layout
	// (always for PDF; for DOCX only when Word saved page-break markers).
	PageNumbersReliable bool `json:"page_numbers_reliable"`
}

// Page is one page of a document.
type Page struct {
	Number     int         `json:"number"`
	Paragraphs []Paragraph `json:"paragraphs"`
}

// Paragraph is a block of text. Section is the most recent heading seen
// before (or at) this paragraph; IsHeading marks headings themselves.
type Paragraph struct {
	Text      string `json:"text"`
	Section   string `json:"section,omitempty"`
	IsHeading bool   `json:"is_heading,omitempty"`
}

// ErrNoText is returned when a document contains no extractable text, e.g. a
// scanned PDF without a text layer.
var ErrNoText = fmt.Errorf("no readable text could be extracted (scanned/image-only documents are not supported)")

// Extract dispatches on the detected file kind.
func Extract(kind upload.Kind, data []byte) (*Document, error) {
	var (
		doc *Document
		err error
	)
	switch kind {
	case upload.KindPDF:
		doc, err = extractPDF(data)
	case upload.KindDOCX:
		doc, err = extractDOCX(data)
	default:
		return nil, fmt.Errorf("unsupported document kind %q", kind)
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(doc.PlainText()) == "" {
		return nil, ErrNoText
	}
	return doc, nil
}

// PlainText returns the whole document as text with blank lines between
// paragraphs.
func (d *Document) PlainText() string {
	var b strings.Builder
	for _, p := range d.Pages {
		for _, para := range p.Paragraphs {
			b.WriteString(para.Text)
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

// PageCount returns the highest page number.
func (d *Document) PageCount() int {
	if len(d.Pages) == 0 {
		return 0
	}
	return d.Pages[len(d.Pages)-1].Number
}

// FindPage returns the page with the given number, or nil.
func (d *Document) FindPage(n int) *Page {
	for i := range d.Pages {
		if d.Pages[i].Number == n {
			return &d.Pages[i]
		}
	}
	return nil
}

// PageText returns all text of a page joined with blank lines.
func (p *Page) PageText() string {
	parts := make([]string, len(p.Paragraphs))
	for i, para := range p.Paragraphs {
		parts[i] = para.Text
	}
	return strings.Join(parts, "\n\n")
}

// Normalize lowercases text and reduces it to letters and digits separated by
// single spaces. It is used to compare quotes against extracted text while
// ignoring punctuation, hyphenation and whitespace differences.
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := true
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// assignSections fills Paragraph.Section from the headings in reading order.
func (d *Document) assignSections() {
	current := ""
	for pi := range d.Pages {
		for i := range d.Pages[pi].Paragraphs {
			p := &d.Pages[pi].Paragraphs[i]
			if p.IsHeading {
				current = p.Text
			}
			p.Section = current
		}
	}
}
