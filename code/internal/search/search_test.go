package search

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTavily(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tvly-x" {
			http.Error(w, "unauthorized", 401)
			return
		}
		io.WriteString(w, `{"results":[
			{"title":"Osmosis","url":"https://example.edu/osmosis","content":"Osmosis is..."},
			{"title":"Bad","url":"javascript:alert(1)","content":"x"}]}`)
	}))
	defer srv.Close()
	tv := &Tavily{APIKey: "tvly-x", HTTP: srv.Client(), Endpoint: srv.URL}
	res, err := tv.Search(context.Background(), "osmosis", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].URL != "https://example.edu/osmosis" {
		t.Fatalf("results = %+v (non-http URLs must be dropped)", res)
	}
}

func TestNoneIsUnavailable(t *testing.T) {
	if _, err := (None{}).Search(context.Background(), "q", 3); !errors.Is(err, ErrUnavailable) {
		t.Fatal("expected ErrUnavailable")
	}
}
