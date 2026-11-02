package extract

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

const wordNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

// extractDOCX reads word/document.xml with the standard library.
//
// DOCX files have no fixed pages; Word records where pages broke the last
// time it laid the document out (w:lastRenderedPageBreak). When those markers
// exist we use them and mark page numbers reliable. Otherwise only explicit
// page breaks are counted and page numbers are flagged as approximate.
func extractDOCX(data []byte) (*Document, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("could not open DOCX: %w", err)
	}
	var docXML, stylesXML []byte
	for _, f := range zr.File {
		switch f.Name {
		case "word/document.xml":
			docXML, err = readZipFile(f)
		case "word/styles.xml":
			stylesXML, err = readZipFile(f)
		}
		if err != nil {
			return nil, fmt.Errorf("could not read DOCX: %w", err)
		}
	}
	if docXML == nil {
		return nil, fmt.Errorf("DOCX has no word/document.xml")
	}
	headingStyles := parseHeadingStyles(stylesXML)
	rendered := bytes.Contains(docXML, []byte("lastRenderedPageBreak"))
	return parseDocumentXML(docXML, headingStyles, rendered)
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	// document.xml of a 10 MB docx can expand a lot; cap to guard against zip bombs.
	return io.ReadAll(io.LimitReader(rc, 200<<20))
}

func attr(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// parseHeadingStyles returns style IDs whose display name is "heading N" or
// "title", which covers localized style IDs (e.g. "berschrift1").
func parseHeadingStyles(stylesXML []byte) map[string]bool {
	ids := map[string]bool{"Title": true}
	for i := 1; i <= 9; i++ {
		ids[fmt.Sprintf("Heading%d", i)] = true
	}
	if stylesXML == nil {
		return ids
	}
	dec := xml.NewDecoder(bytes.NewReader(stylesXML))
	currentID := ""
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != wordNS {
			continue
		}
		switch se.Name.Local {
		case "style":
			currentID = attr(se, "styleId")
		case "name":
			name := strings.ToLower(attr(se, "val"))
			if currentID != "" && (strings.HasPrefix(name, "heading") || name == "title") {
				ids[currentID] = true
			}
		case "outlineLvl":
			if currentID != "" {
				ids[currentID] = true
			}
		}
	}
	return ids
}

func parseDocumentXML(docXML []byte, headingStyles map[string]bool, rendered bool) (*Document, error) {
	doc := &Document{PageNumbersReliable: rendered}
	page := &Page{Number: 1}

	var (
		inPara    bool
		inPPr     bool
		inText    bool
		isHeading bool
		text      strings.Builder
	)
	emit := func() {
		t := strings.Join(strings.Fields(text.String()), " ")
		if t != "" {
			page.Paragraphs = append(page.Paragraphs, Paragraph{Text: t, IsHeading: isHeading})
		}
		text.Reset()
	}
	newPage := func() {
		emit()
		doc.Pages = append(doc.Pages, *page)
		page = &Page{Number: page.Number + 1}
	}

	dec := xml.NewDecoder(bytes.NewReader(docXML))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("malformed DOCX XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wordNS {
				continue
			}
			switch t.Name.Local {
			case "p":
				inPara, isHeading = true, false
				text.Reset()
			case "pPr":
				inPPr = true
			case "pStyle":
				if inPPr && headingStyles[attr(t, "val")] {
					isHeading = true
				}
			case "outlineLvl":
				if inPPr {
					isHeading = true
				}
			case "pageBreakBefore":
				if inPPr && !rendered && attr(t, "val") != "false" && attr(t, "val") != "0" && len(page.Paragraphs) > 0 {
					newPage()
				}
			case "t":
				inText = true
			case "tab":
				if !inPPr {
					text.WriteByte(' ')
				}
			case "br", "cr":
				if attr(t, "type") == "page" && !rendered {
					newPage()
				} else {
					text.WriteByte(' ')
				}
			case "lastRenderedPageBreak":
				if rendered && (len(page.Paragraphs) > 0 || strings.TrimSpace(text.String()) != "") {
					newPage()
				}
			}
		case xml.EndElement:
			if t.Name.Space != wordNS {
				continue
			}
			switch t.Name.Local {
			case "p":
				if inPara {
					emit()
				}
				inPara = false
			case "pPr":
				inPPr = false
			case "t":
				inText = false
			}
		case xml.CharData:
			if inText {
				text.Write(t)
			}
		}
	}
	doc.Pages = append(doc.Pages, *page)
	doc.assignSections()
	return doc, nil
}
