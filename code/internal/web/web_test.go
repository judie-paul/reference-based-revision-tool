package web

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"revisiontool/internal/config"
	"revisiontool/internal/llm"
	"revisiontool/internal/search"
	"revisiontool/internal/store"
	"revisiontool/internal/testdocs"
)

func testServer(t *testing.T, fake *llm.Fake, cfg config.Config) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv, err := New(cfg, st, fake, nil, search.None{})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func defaultCfg() config.Config {
	return config.Config{
		MaxReferenceFileBytes: 1 << 20, MaxPaperFileBytes: 1 << 20, MaxReferenceDocs: 2,
		LLMContextWindow: 200000, LLMMaxOutputTokens: 4000, SearchMaxResults: 3,
	}
}

func uploadBody(t *testing.T, refs int) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("title", "Biology test")
	w, _ := mw.CreateFormFile("paper", "paper.pdf")
	w.Write(testdocs.PDF([][]testdocs.Line{{{Text: "1. Name the organelle where photosynthesis takes place."}}}))
	for i := 0; i < refs; i++ {
		w, _ = mw.CreateFormFile("references", "notes.docx")
		w.Write(testdocs.DOCX([]testdocs.Para{
			{Text: "Cells", Style: "Heading1"},
			{Text: "The chloroplast is the organelle where photosynthesis takes place in plant cells."},
		}))
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestFullFlow(t *testing.T) {
	fake := &llm.Fake{Replies: []string{
		`{"questions":[{"number":"1","parent_number":"","text":"Name the organelle where photosynthesis takes place.","marks":""}],"problems":[]}`,
		`{"coverage":"complete","segments":[{"text":"The chloroplast.","citations":[{"document_id":"D1","page":1,"section":"",
		  "quote":"The chloroplast is the organelle where photosynthesis takes place"}]}],
		  "missing_information":"","search_queries":[],"contradictions":"","ambiguity":""}`,
	}}
	ts := testServer(t, fake, defaultCfg())
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body, ctype := uploadBody(t, 1)
	resp, err := client.Post(ts.URL+"/sessions", ctype, body)
	if err != nil {
		t.Fatal(err)
	}
	page := readAll(t, resp)
	if !strings.Contains(page, "Name the organelle where photosynthesis") {
		t.Fatalf("session page missing question:\n%s", page)
	}
	sessionURL := resp.Request.URL.String()

	// Find the question link and generate an answer.
	i := strings.Index(page, "/questions/")
	qpath := page[strings.LastIndex(page[:i], `"`)+1 : i+len("/questions/")+strings.Index(page[i+len("/questions/"):], `"`)]
	resp, err = client.Post(ts.URL+qpath+"/answer", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	page = readAll(t, resp)
	for _, want := range []string{"DOCUMENT-BASED", "notes.docx", "quote verified", "The chloroplast."} {
		if !strings.Contains(page, want) {
			t.Errorf("question page missing %q", want)
		}
	}
	if strings.Contains(page, `C:\`) || strings.Contains(page, "/tmp/") {
		t.Error("internal paths leaked into page")
	}
	resp, _ = client.Get(sessionURL)
	if !strings.Contains(readAll(t, resp), "DOCUMENT-BASED") {
		t.Error("session overview should show answer status")
	}
}

func TestUploadLimits(t *testing.T) {
	ts := testServer(t, &llm.Fake{}, defaultCfg())
	body, ctype := uploadBody(t, 3) // limit is 2
	resp, err := http.Post(ts.URL+"/sessions", ctype, body)
	if err != nil {
		t.Fatal(err)
	}
	page := readAll(t, resp)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(page, "maximum per session is 2") {
		t.Fatalf("status %d, page:\n%s", resp.StatusCode, page)
	}
}

func TestContextBudgetRejectsUpload(t *testing.T) {
	cfg := defaultCfg()
	cfg.LLMContextWindow = 6100 // smaller than allowances + reference
	ts := testServer(t, &llm.Fake{}, cfg)
	body, ctype := uploadBody(t, 1)
	resp, err := http.Post(ts.URL+"/sessions", ctype, body)
	if err != nil {
		t.Fatal(err)
	}
	page := readAll(t, resp)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(page, "too large for the configured model") {
		t.Fatalf("status %d, page:\n%s", resp.StatusCode, page)
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	ts := testServer(t, &llm.Fake{}, defaultCfg())
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/sessions/1/delete", nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
