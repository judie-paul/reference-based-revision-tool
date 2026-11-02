package store

import (
	"path/filepath"
	"testing"

	"revisiontool/internal/extract"
)

func TestRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	content := &extract.Document{Pages: []extract.Page{{Number: 1, Paragraphs: []extract.Paragraph{{Text: "x"}}}}}
	docs := []*Document{
		{Role: "paper", Filename: "paper.pdf", Kind: "pdf", SizeBytes: 10, Content: content},
		{Role: "reference", Filename: "notes.docx", Kind: "docx", SizeBytes: 20, Content: &extract.Document{PageNumbersReliable: false}},
	}
	sid, err := st.CreateSession("Biology", docs, []int{0, 1234})
	if err != nil {
		t.Fatal(err)
	}

	qs := []Question{
		{Number: "1", ParentSeq: -1, Text: "Cells", Verified: true},
		{Number: "1(a)", ParentSeq: 0, Text: "Define a cell.", Verified: true},
	}
	if err := st.SetQuestions(sid, qs, []string{"stray text"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Questions(sid)
	if err != nil || len(got) != 2 {
		t.Fatalf("questions: %v %v", got, err)
	}
	if got[1].ParentID != got[0].ID || got[1].Depth != 1 {
		t.Errorf("parent/depth wrong: %+v", got[1])
	}

	a := &Answer{QuestionID: got[1].ID, Status: StatusSupplemented, Segments: []Segment{
		{Text: "A cell is...", Basis: BasisDocument, Sources: []Source{{Kind: "document", DocumentID: docs[1].ID, Page: 3, Verified: true}}},
		{Text: "Also...", Basis: BasisExternal, Sources: []Source{{Kind: "external", Title: "T", URL: "https://example.org"}}},
	}}
	if err := st.SaveAnswer(a); err != nil {
		t.Fatal(err)
	}
	// Regenerating replaces the previous answer.
	if err := st.SaveAnswer(a); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.GetAnswer(got[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Segments) != 2 || len(loaded.Segments[0].Sources) != 1 {
		t.Fatalf("loaded = %+v", loaded)
	}
	src := loaded.Segments[0].Sources[0]
	if src.Filename != "notes.docx" || src.Page != 3 || !src.PageApprox || !src.Verified {
		t.Errorf("document source = %+v", src)
	}
	if loaded.Segments[1].Sources[0].URL != "https://example.org" {
		t.Errorf("external source = %+v", loaded.Segments[1].Sources[0])
	}

	se, err := st.GetSession(sid)
	if err != nil {
		t.Fatal(err)
	}
	if se.ReferenceTokens != 1234 || se.QuestionCount != 2 || se.AnsweredCount != 1 || len(se.UnassignedText) != 1 {
		t.Errorf("session = %+v", se)
	}

	if err := st.DeleteSession(sid); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetAnswer(got[1].ID); err != ErrNotFound {
		t.Errorf("expected cascade delete, got %v", err)
	}
}
