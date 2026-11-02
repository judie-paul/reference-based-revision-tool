package questions

import (
	"context"
	"strings"
	"testing"

	"revisiontool/internal/extract"
	"revisiontool/internal/llm"
)

func paper() *extract.Document {
	return &extract.Document{Pages: []extract.Page{{Number: 1, Paragraphs: []extract.Paragraph{
		{Text: "BIOLOGY PAPER 1. Answer all questions in the spaces provided."},
		{Text: "1. The diagram below shows a plant cell."},
		{Text: "(a) Name the organelle responsible for photosynthesis. (2 marks)"},
		{Text: "(b) Explain how the cell wall supports the plant. (4 marks)"},
		{Text: "2. Describe the process of osmosis in root hair cells and explain its importance to the plant."},
	}}}}
}

func TestExtractBuildsHierarchyAndVerifies(t *testing.T) {
	fake := &llm.Fake{Replies: []string{`{"questions":[
		{"number":"1","parent_number":"","text":"The diagram below shows a plant cell.","marks":""},
		{"number":"1(a)","parent_number":"1","text":"Name the organelle responsible for photosynthesis.","marks":"2 marks"},
		{"number":"1(b)","parent_number":"1","text":"Explain how the root absorbs mineral salts from soil.","marks":"4 marks"}
	],"problems":[]}`}}

	res, err := Extract(context.Background(), fake, paper(), 100000, 1000)
	if err != nil {
		t.Fatal(err)
	}
	qs := res.Questions
	if len(qs) != 3 {
		t.Fatalf("got %d questions", len(qs))
	}
	if qs[1].ParentSeq != 0 || qs[2].ParentSeq != 0 || qs[0].ParentSeq != -1 {
		t.Errorf("hierarchy wrong: %+v", qs)
	}
	if !qs[0].Verified || !qs[1].Verified {
		t.Error("real questions should verify")
	}
	if qs[2].Verified {
		t.Error("a question whose text is not in the paper must be flagged")
	}
	// Question 2 was omitted by the model: it must be reported, not silently lost.
	joined := strings.Join(res.Notes, "\n")
	if !strings.Contains(joined, "osmosis in root hair cells") {
		t.Errorf("omitted question not reported; notes = %v", res.Notes)
	}
}

func TestExtractRejectsOversizedPaper(t *testing.T) {
	_, err := Extract(context.Background(), &llm.Fake{}, paper(), 50, 10)
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("expected context error, got %v", err)
	}
}
