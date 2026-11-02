// Package testdocs builds small PDF and DOCX files in memory for tests.
package testdocs

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
)

// Line is one line of text on a PDF page.
type Line struct {
	Text string
	Size float64 // font size; 0 means 11
}

// PDF builds a minimal valid PDF where each element of pages is a page made
// of the given lines (Helvetica, top to bottom). A Line with empty Text adds
// extra vertical space (paragraph break).
func PDF(pages [][]Line) []byte {
	var objs []string
	// 1: catalog, 2: pages, 3: font, then page/content pairs.
	nPages := len(pages)
	kids := make([]string, nPages)
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 4+2*i)
	}
	objs = append(objs,
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), nPages),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	)
	for i, lines := range pages {
		var cs strings.Builder
		y := 760.0
		for _, l := range lines {
			size := l.Size
			if size == 0 {
				size = 11
			}
			if l.Text == "" {
				y -= 20
				continue
			}
			esc := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(l.Text)
			fmt.Fprintf(&cs, "BT /F1 %.0f Tf 72 %.0f Td (%s) Tj ET\n", size, y, esc)
			y -= size * 1.3
		}
		content := cs.String()
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i),
			fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		)
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

// Para is one DOCX paragraph.
type Para struct {
	Text  string
	Style string // e.g. "Heading1"; empty for normal text
	// PageBreak inserts a rendered page break before this paragraph, as
	// Word does when it saves a laid-out document.
	PageBreak bool
}

// DOCX builds a minimal .docx file.
func DOCX(paras []Para) []byte {
	var body strings.Builder
	for _, p := range paras {
		body.WriteString("<w:p>")
		if p.Style != "" {
			fmt.Fprintf(&body, `<w:pPr><w:pStyle w:val="%s"/></w:pPr>`, p.Style)
		}
		body.WriteString("<w:r>")
		if p.PageBreak {
			body.WriteString("<w:lastRenderedPageBreak/>")
		}
		fmt.Fprintf(&body, `<w:t xml:space="preserve">%s</w:t></w:r></w:p>`, xmlEscape(p.Text))
	}
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		body.String() + `</w:body></w:document>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"word/document.xml":   doc,
	}
	for _, name := range []string{"[Content_Types].xml", "word/document.xml"} {
		w, _ := zw.Create(name)
		w.Write([]byte(files[name]))
	}
	zw.Close()
	return buf.Bytes()
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
