package answer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"revisiontool/internal/extract"
	"revisiontool/internal/llm"
	"revisiontool/internal/search"
	"revisiontool/internal/store"
)

func refDocs() *Reference {
	doc := &store.Document{ID: 42, Filename: "biology-notes.pdf", Content: &extract.Document{
		PageNumbersReliable: true,
		Pages: []extract.Page{
			{Number: 1, Paragraphs: []extract.Paragraph{
				{Text: "Cell Structure", IsHeading: true, Section: "Cell Structure"},
				{Text: "The chloroplast is the organelle where photosynthesis takes place in plant cells.", Section: "Cell Structure"},
			}},
			{Number: 2, Paragraphs: []extract.Paragraph{
				{Text: "Transport", IsHeading: true, Section: "Transport"},
				{Text: "Osmosis is the movement of water molecules across a partially permeable membrane.", Section: "Transport"},
			}},
		},
	}}
	return BuildReference([]*store.Document{doc})
}

type fakeSearch struct {
	results []search.Result
	calls   int
}

func (f *fakeSearch) Search(context.Context, string, int) ([]search.Result, error) {
	f.calls++
	return f.results, nil
}
func (f *fakeSearch) Available() bool { return true }
func (f *fakeSearch) Name() string    { return "fake" }

func svc(l llm.Client, s search.Provider) *Service {
	return &Service{LLM: l, Search: s, ContextWindow: 200000, MaxOutputTokens: 4000, SearchMaxResults: 5}
}

var q = &store.Question{ID: 7, Number: "1(a)", Text: "Name the organelle where photosynthesis occurs."}

func TestDocumentBasedWithPageCorrection(t *testing.T) {
	// The model cites page 2, but the quote is really on page 1.
	fake := &llm.Fake{Replies: []string{`{"coverage":"complete","segments":[{"text":"The chloroplast.",
		"citations":[{"document_id":"D1","page":2,"section":"Wrong","quote":"The chloroplast is the organelle where photosynthesis takes place"}]}],
		"missing_information":"","search_queries":[],"contradictions":"","ambiguity":""}`}}
	fs := &fakeSearch{}
	a, err := svc(fake, fs).Generate(context.Background(), refDocs(), nil, q)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != store.StatusDocument {
		t.Fatalf("status = %s", a.Status)
	}
	if fs.calls != 0 || len(fake.Requests) != 1 {
		t.Error("no external search should happen when documents suffice")
	}
	src := a.Segments[0].Sources[0]
	if !src.Verified || src.Page != 1 || src.Section != "Cell Structure" || src.DocumentID != 42 {
		t.Errorf("source = %+v", src)
	}
	if !strings.Contains(fake.Requests[0].Reference, `<page number="1">`) {
		t.Error("reference material must be sent with page markers")
	}
}

func TestSupplementedUsesOnlyRealResults(t *testing.T) {
	fake := &llm.Fake{Replies: []string{
		`{"coverage":"partial","segments":[{"text":"Osmosis moves water across a membrane.",
		  "citations":[{"document_id":"D1","page":2,"section":"Transport","quote":"movement of water molecules across a partially permeable membrane"}]}],
		  "missing_information":"importance of osmosis to root hair cells","search_queries":["osmosis root hair cells importance"],
		  "contradictions":"","ambiguity":""}`,
		`{"segments":[{"text":"Root hair cells absorb water by osmosis.","source_ids":["E1","E9"]},
		  {"text":"Invented claim.","source_ids":["E7"]}],"insufficient":false,"conflicts":""}`,
	}}
	fs := &fakeSearch{results: []search.Result{{Title: "Root hairs", URL: "https://example.edu/roots", Content: "Root hair cells take up water by osmosis."}}}
	a, err := svc(fake, fs).Generate(context.Background(), refDocs(), nil, q)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != store.StatusSupplemented {
		t.Fatalf("status = %s", a.Status)
	}
	if len(a.Segments) != 3 {
		t.Fatalf("segments = %+v", a.Segments)
	}
	ext := a.Segments[1]
	if ext.Basis != store.BasisExternal || len(ext.Sources) != 1 || ext.Sources[0].URL != "https://example.edu/roots" {
		t.Errorf("external segment = %+v", ext)
	}
	if a.Segments[2].Basis != store.BasisUnsupported || len(a.Segments[2].Sources) != 0 {
		t.Errorf("segment citing a non-existent result must be unsupported: %+v", a.Segments[2])
	}
	// The second call must not resend the reference documents.
	if fake.Requests[1].Reference != "" {
		t.Error("external completion should not resend documents")
	}
}

func TestNoSearchConfiguredIsHonest(t *testing.T) {
	fake := &llm.Fake{Replies: []string{`{"coverage":"none","segments":[],"missing_information":"everything",
		"search_queries":["x"],"contradictions":"","ambiguity":""}`}}
	a, err := svc(fake, search.None{}).Generate(context.Background(), refDocs(), nil, q)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != store.StatusUnanswered {
		t.Fatalf("status = %s", a.Status)
	}
	if !strings.Contains(a.Notice, "not configured") {
		t.Errorf("notice = %q", a.Notice)
	}
}

func TestFabricatedQuoteIsUnverified(t *testing.T) {
	fake := &llm.Fake{Replies: []string{`{"coverage":"complete","segments":[{"text":"Mitochondria do photosynthesis.",
		"citations":[{"document_id":"D1","page":1,"section":"","quote":"mitochondria are the site of photosynthesis"},
		             {"document_id":"D9","page":1,"section":"","quote":"made up document"}]}],
		"missing_information":"","search_queries":[],"contradictions":"","ambiguity":""}`}}
	a, err := svc(fake, search.None{}).Generate(context.Background(), refDocs(), nil, q)
	if err != nil {
		t.Fatal(err)
	}
	seg := a.Segments[0]
	if seg.Basis != store.BasisUnsupported || len(seg.Sources) != 1 || seg.Sources[0].Verified {
		t.Errorf("segment = %+v", seg)
	}
	if a.Status != store.StatusUnanswered {
		t.Errorf("unverified-only answer must not be DOCUMENT-BASED, got %s", a.Status)
	}
}

func TestBudgetExceeded(t *testing.T) {
	s := svc(&llm.Fake{}, nil)
	s.ContextWindow = 1000
	_, err := s.Generate(context.Background(), refDocs(), nil, q)
	var be *BudgetError
	if !errors.As(err, &be) {
		t.Fatalf("expected BudgetError, got %v", err)
	}
}
