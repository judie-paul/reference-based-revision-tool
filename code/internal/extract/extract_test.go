package extract

import (
	"strings"
	"testing"

	"revisiontool/internal/testdocs"
	"revisiontool/internal/upload"
)

func TestDOCXPagesAndSections(t *testing.T) {
	data := testdocs.DOCX([]testdocs.Para{
		{Text: "Photosynthesis", Style: "Heading1"},
		{Text: "Plants convert light energy into chemical energy."},
		{Text: "Respiration", Style: "Heading1", PageBreak: true},
		{Text: "Cells release energy from glucose."},
	})
	doc, err := Extract(upload.KindDOCX, data)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.PageNumbersReliable {
		t.Error("expected reliable page numbers when rendered breaks exist")
	}
	if len(doc.Pages) != 2 {
		t.Fatalf("got %d pages, want 2", len(doc.Pages))
	}
	p2 := doc.FindPage(2)
	if p2 == nil || len(p2.Paragraphs) != 2 {
		t.Fatalf("page 2 = %+v", p2)
	}
	if got := p2.Paragraphs[1].Section; got != "Respiration" {
		t.Errorf("section = %q, want Respiration", got)
	}
	if !p2.Paragraphs[0].IsHeading {
		t.Error("expected heading flag")
	}
}

func TestDOCXWithoutRenderedBreaksIsApproximate(t *testing.T) {
	doc, err := Extract(upload.KindDOCX, testdocs.DOCX([]testdocs.Para{{Text: "Only text."}}))
	if err != nil {
		t.Fatal(err)
	}
	if doc.PageNumbersReliable {
		t.Error("page numbers should be flagged approximate")
	}
}

func TestPDFPagesParagraphsHeadings(t *testing.T) {
	data := testdocs.PDF([][]testdocs.Line{
		{
			{Text: "Chapter 1 Cells", Size: 18},
			{Text: ""},
			{Text: "The cell is the basic unit of life."},
			{Text: "All organisms are made of cells."},
		},
		{
			{Text: "Mitochondria produce ATP through respiration."},
		},
	})
	doc, err := Extract(upload.KindPDF, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 2 {
		t.Fatalf("got %d pages", len(doc.Pages))
	}
	p1 := doc.Pages[0]
	if len(p1.Paragraphs) < 2 || !p1.Paragraphs[0].IsHeading {
		t.Fatalf("page 1 paragraphs = %+v", p1.Paragraphs)
	}
	body := p1.Paragraphs[1]
	if !strings.Contains(body.Text, "basic unit of life") || !strings.Contains(body.Text, "made of cells") {
		t.Errorf("body paragraph = %q", body.Text)
	}
	if body.Section != "Chapter 1 Cells" {
		t.Errorf("section = %q", body.Section)
	}
	if !strings.Contains(doc.Pages[1].PageText(), "Mitochondria produce ATP") {
		t.Errorf("page 2 text = %q", doc.Pages[1].PageText())
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize("  The Cell—is   BASIC, (unit)!"); got != "the cell is basic unit" {
		t.Errorf("Normalize = %q", got)
	}
}
