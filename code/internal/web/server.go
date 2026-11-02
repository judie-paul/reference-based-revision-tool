// Package web contains the HTTP handlers and server-rendered HTML templates.
// Handlers stay thin: they parse requests, call the workflow packages and
// render results.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"revisiontool/internal/answer"
	"revisiontool/internal/config"
	"revisiontool/internal/llm"
	"revisiontool/internal/search"
	"revisiontool/internal/store"
	"revisiontool/internal/upload"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Server wires configuration, storage and the workflow services together.
type Server struct {
	cfg    config.Config
	store  *store.Store
	llm    llm.Client // nil when no API key is configured
	llmErr string
	search search.Provider
	answer *answer.Service
	pages  map[string]*template.Template

	mu       sync.Mutex
	inFlight map[int64]bool // question IDs currently being answered
}

// New creates the server. llmClient may be nil; llmErr then explains why.
func New(cfg config.Config, st *store.Store, llmClient llm.Client, llmErr error, sp search.Provider) (*Server, error) {
	s := &Server{
		cfg:      cfg,
		store:    st,
		llm:      llmClient,
		search:   sp,
		inFlight: map[int64]bool{},
		answer: &answer.Service{
			LLM:              llmClient,
			Search:           sp,
			ContextWindow:    cfg.LLMContextWindow,
			MaxOutputTokens:  cfg.LLMMaxOutputTokens,
			SearchMaxResults: cfg.SearchMaxResults,
		},
	}
	if llmErr != nil {
		s.llmErr = llmErr.Error()
	}
	funcs := template.FuncMap{
		"statusClass": statusClass,
		"humanSize":   upload.HumanSize,
		"thousands":   thousands,
	}
	s.pages = map[string]*template.Template{}
	for _, page := range []string{"home", "session", "question"} {
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/"+page+".html")
		if err != nil {
			return nil, err
		}
		s.pages[page] = t
	}
	return s, nil
}

// Handler returns the application's HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("POST /sessions", s.handleCreateSession)
	mux.HandleFunc("GET /sessions/{id}", s.handleSession)
	mux.HandleFunc("POST /sessions/{id}/extract", s.handleExtract)
	mux.HandleFunc("POST /sessions/{id}/delete", s.handleDelete)
	mux.HandleFunc("GET /sessions/{id}/questions/{qid}", s.handleQuestion)
	mux.HandleFunc("POST /sessions/{id}/questions/{qid}/answer", s.handleAnswer)
	return sameOrigin(securityHeaders(mux))
}

func (s *Server) render(w http.ResponseWriter, status int, page string, data map[string]any) {
	data["LLMReady"] = s.llm != nil
	data["LLMError"] = s.llmErr
	data["SearchReady"] = s.search.Available()
	data["SearchName"] = s.search.Name()
	if s.llm != nil {
		data["ModelName"] = s.llm.Model()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.pages[page].Execute(w, data); err != nil {
		log.Printf("render %s: %v", page, err)
	}
}

// fail logs internal errors and shows a generic message, so internal
// details such as file paths never reach the browser.
func (s *Server) fail(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	http.Error(w, "Something went wrong. Check the server log for details.", http.StatusInternalServerError)
}

func statusClass(status string) string {
	switch status {
	case store.StatusDocument:
		return "st-document"
	case store.StatusSupplemented:
		return "st-supplemented"
	case store.StatusExternal:
		return "st-external"
	case store.StatusUnanswered:
		return "st-unanswered"
	}
	return "st-none"
}

// thousands formats 1234567 as "1,234,567".
func thousands(n int) string {
	s := strconv.Itoa(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

// sameOrigin rejects cross-site form posts (a basic CSRF guard).
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if origin := r.Header.Get("Origin"); origin != "" && origin != "null" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					http.Error(w, "cross-origin request rejected", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
