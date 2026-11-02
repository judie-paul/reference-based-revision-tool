package extract

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
)

// pdfLine is one visual line of text on a page.
type pdfLine struct {
	text     string
	y        float64
	fontSize float64
	bold     bool
}

func extractPDF(data []byte) (doc *Document, err error) {
	defer func() {
		if r := recover(); r != nil {
			doc, err = nil, fmt.Errorf("could not read PDF: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("could not read PDF: %w", err)
	}

	n := r.NumPage()
	pageLines := make([][]pdfLine, n)
	fallback := make([]string, n)
	var sizes []float64
	for i := 1; i <= n; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		lines := pageToLines(p)
		if len(lines) == 0 {
			// Layout-based extraction failed; keep the raw text stream so
			// the page is not silently lost.
			if txt, e := p.GetPlainText(nil); e == nil {
				fallback[i-1] = txt
			}
		}
		for _, l := range lines {
			sizes = append(sizes, l.fontSize)
		}
		pageLines[i-1] = lines
	}

	body := median(sizes)
	doc = &Document{PageNumbersReliable: true}
	for i := 0; i < n; i++ {
		page := Page{Number: i + 1}
		if len(pageLines[i]) > 0 {
			page.Paragraphs = linesToParagraphs(pageLines[i], body)
		} else if strings.TrimSpace(fallback[i]) != "" {
			page.Paragraphs = splitParagraphs(fallback[i])
		}
		doc.Pages = append(doc.Pages, page)
	}
	doc.assignSections()
	return doc, nil
}

// pageToLines groups the positioned glyphs of a page into lines, ordered top
// to bottom, inserting spaces where glyphs are visibly separated.
func pageToLines(p pdf.Page) (lines []pdfLine) {
	defer func() {
		if recover() != nil {
			lines = nil
		}
	}()
	glyphs := p.Content().Text
	if len(glyphs) == 0 {
		return nil
	}
	sort.SliceStable(glyphs, func(i, j int) bool { return glyphs[i].Y > glyphs[j].Y })

	var group []pdf.Text
	flush := func() {
		if len(group) == 0 {
			return
		}
		sort.SliceStable(group, func(i, j int) bool { return group[i].X < group[j].X })
		var b strings.Builder
		var sizeSum float64
		boldCount := 0
		for k, g := range group {
			if k > 0 {
				prev := group[k-1]
				w := prev.W
				if w <= 0 {
					w = prev.FontSize * 0.5
				}
				gap := g.X - (prev.X + w)
				if gap > 0.2*math.Max(g.FontSize, 1) && !strings.HasSuffix(b.String(), " ") && !strings.HasPrefix(g.S, " ") {
					b.WriteByte(' ')
				}
			}
			b.WriteString(g.S)
			sizeSum += g.FontSize
			if strings.Contains(strings.ToLower(g.Font), "bold") {
				boldCount++
			}
		}
		text := strings.Join(strings.Fields(b.String()), " ")
		if text != "" {
			lines = append(lines, pdfLine{
				text:     text,
				y:        group[0].Y,
				fontSize: sizeSum / float64(len(group)),
				bold:     boldCount*2 > len(group),
			})
		}
		group = group[:0]
	}

	for _, g := range glyphs {
		if len(group) > 0 {
			tol := math.Max(group[0].FontSize*0.3, 1.5)
			if math.Abs(group[0].Y-g.Y) > tol {
				flush()
			}
		}
		group = append(group, g)
	}
	flush()
	return lines
}

// linesToParagraphs merges consecutive lines into paragraphs. A new paragraph
// starts at a large vertical gap or a change in font size; short lines set
// in a noticeably larger (or bold) font are treated as headings.
func linesToParagraphs(lines []pdfLine, bodySize float64) []Paragraph {
	isHeading := func(l pdfLine) bool {
		if len(l.text) > 120 || bodySize <= 0 {
			return false
		}
		return l.fontSize >= bodySize*1.15 || (l.bold && len(l.text) < 80 && !strings.HasSuffix(l.text, "."))
	}

	var paras []Paragraph
	var cur []string
	curHeading := false
	flush := func() {
		if len(cur) > 0 {
			paras = append(paras, Paragraph{Text: joinLines(cur), IsHeading: curHeading})
		}
		cur = nil
	}
	for i, l := range lines {
		h := isHeading(l)
		if i > 0 {
			prev := lines[i-1]
			gap := prev.y - l.y
			lineHeight := math.Max(prev.fontSize, 1) * 1.6
			sizeChange := math.Abs(prev.fontSize-l.fontSize) > 0.15*math.Max(prev.fontSize, 1)
			if gap > lineHeight || gap < 0 || sizeChange || h != curHeading {
				flush()
			}
		}
		if len(cur) == 0 {
			curHeading = h
		}
		cur = append(cur, l.text)
	}
	flush()
	return paras
}

// joinLines joins wrapped lines, repairing words hyphenated across lines.
func joinLines(lines []string) string {
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			prev := b.String()
			if strings.HasSuffix(prev, "-") && len(prev) > 1 && isLower(l) {
				s := strings.TrimSuffix(prev, "-")
				b.Reset()
				b.WriteString(s)
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteString(l)
	}
	return b.String()
}

func isLower(s string) bool {
	for _, r := range s {
		return r >= 'a' && r <= 'z'
	}
	return false
}

// splitParagraphs splits raw text on blank lines.
func splitParagraphs(s string) []Paragraph {
	var out []Paragraph
	for _, chunk := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n\n") {
		t := strings.Join(strings.Fields(chunk), " ")
		if t != "" {
			out = append(out, Paragraph{Text: t})
		}
	}
	return out
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	return c[len(c)/2]
}
