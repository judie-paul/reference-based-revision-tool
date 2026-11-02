package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"revisiontool/internal/answer"
	"revisiontool/internal/extract"
	"revisiontool/internal/questions"
	"revisiontool/internal/store"
	"revisiontool/internal/upload"
)

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	s.renderHome(w, http.StatusOK, "")
}

func (s *Server) renderHome(w http.ResponseWriter, status int, errMsg string) {
	sessions, err := s.store.ListSessions()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, status, "home", map[string]any{
		"Title":         "Revision sessions",
		"Sessions":      sessions,
		"Error":         errMsg,
		"MaxRefDocs":    s.cfg.MaxReferenceDocs,
		"MaxRefSize":    s.cfg.MaxReferenceFileBytes,
		"MaxPaperSize":  s.cfg.MaxPaperFileBytes,
		"ContextWindow": s.cfg.LLMContextWindow,
	})
}

// userError is an error whose message is safe and useful to show.
type userError struct{ msg string }

func (e *userError) Error() string { return e.msg }

func userErr(format string, a ...any) error { return &userError{fmt.Sprintf(format, a...)} }

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	sid, err := s.createSession(w, r)
	if err != nil {
		var ue *userError
		var ve *upload.ValidationError
		var be *answer.BudgetError
		switch {
		case errors.As(err, &ue), errors.As(err, &ve), errors.As(err, &be):
			s.renderHome(w, http.StatusBadRequest, err.Error())
		default:
			s.fail(w, err)
		}
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/sessions/%d", sid), http.StatusSeeOther)
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) (int64, error) {
	if s.llm == nil {
		return 0, userErr("The LLM is not configured: %s", s.llmErr)
	}
	limit := s.cfg.MaxPaperFileBytes + int64(s.cfg.MaxReferenceDocs)*s.cfg.MaxReferenceFileBytes + 1<<20
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return 0, userErr("The upload is too large. Each reference document may be at most %s, the question paper at most %s, with at most %d reference documents.",
				upload.HumanSize(s.cfg.MaxReferenceFileBytes), upload.HumanSize(s.cfg.MaxPaperFileBytes), s.cfg.MaxReferenceDocs)
		}
		return 0, userErr("The upload could not be read. Please try again.")
	}
	defer r.MultipartForm.RemoveAll()

	papers := r.MultipartForm.File["paper"]
	refs := r.MultipartForm.File["references"]
	if len(papers) != 1 {
		return 0, userErr("Please choose exactly one question paper.")
	}
	if len(refs) == 0 {
		return 0, userErr("Please choose at least one reference document.")
	}
	if len(refs) > s.cfg.MaxReferenceDocs {
		return 0, userErr("You selected %d reference documents; the maximum per session is %d.", len(refs), s.cfg.MaxReferenceDocs)
	}

	paper, err := loadDocument(papers[0], "paper", s.cfg.MaxPaperFileBytes)
	if err != nil {
		return 0, err
	}
	docs := []*store.Document{paper}
	tokens := []int{0}
	var refDocs []*store.Document
	for _, fh := range refs {
		d, err := loadDocument(fh, "reference", s.cfg.MaxReferenceFileBytes)
		if err != nil {
			return 0, err
		}
		docs = append(docs, d)
		refDocs = append(refDocs, d)
		tokens = append(tokens, answer.BuildReference([]*store.Document{d}).Tokens())
	}

	// All reference material must fit; nothing is ever truncated.
	if err := answer.CheckBudget(answer.BuildReference(refDocs).Tokens(), s.cfg.LLMContextWindow, s.cfg.LLMMaxOutputTokens); err != nil {
		return 0, err
	}

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = paper.Filename
	}
	if len(title) > 200 {
		title = title[:200]
	}
	sid, err := s.store.CreateSession(title, docs, tokens)
	if err != nil {
		return 0, err
	}
	s.extractQuestions(r.Context(), sid, paper)
	return sid, nil
}

// loadDocument reads, validates and extracts one uploaded file.
func loadDocument(fh *multipart.FileHeader, role string, maxBytes int64) (*store.Document, error) {
	name := upload.SafeName(fh.Filename)
	if fh.Size > maxBytes {
		return nil, userErr("%s is %s, which exceeds the %s limit.", name, upload.HumanSize(fh.Size), upload.HumanSize(maxBytes))
	}
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	kind, err := upload.Validate(name, data, maxBytes)
	if err != nil {
		return nil, err
	}
	content, err := extract.Extract(kind, data)
	if err != nil {
		return nil, userErr("%s: %v", name, err)
	}
	return &store.Document{Role: role, Filename: name, Kind: string(kind), SizeBytes: int64(len(data)), Content: content}, nil
}

func (s *Server) extractQuestions(ctx context.Context, sid int64, paper *store.Document) {
	res, err := questions.Extract(ctx, s.llm, paper.Content, s.cfg.LLMContextWindow, s.cfg.LLMMaxOutputTokens)
	if err == nil && len(res.Questions) == 0 {
		err = errors.New("no questions could be identified in the question paper")
	}
	if err != nil {
		log.Printf("session %d: question extraction failed: %v", sid, err)
		if e := s.store.SetQuestionsFailed(sid, err.Error()); e != nil {
			log.Printf("session %d: %v", sid, e)
		}
		return
	}
	if err := s.store.SetQuestions(sid, res.Questions, res.Notes); err != nil {
		log.Printf("session %d: saving questions: %v", sid, err)
	}
}

func (s *Server) handleExtract(w http.ResponseWriter, r *http.Request) {
	sid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if s.llm == nil {
		http.Error(w, "LLM not configured", http.StatusServiceUnavailable)
		return
	}
	papers, err := s.store.Documents(sid, "paper")
	if err != nil || len(papers) == 0 {
		http.NotFound(w, r)
		return
	}
	s.extractQuestions(r.Context(), sid, papers[0])
	http.Redirect(w, r, fmt.Sprintf("/sessions/%d", sid), http.StatusSeeOther)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	sid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.store.DeleteSession(sid); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// questionRow is a question with display information.
type questionRow struct {
	*store.Question
	Status  string
	IsGroup bool // has sub-questions; answered through its children
}

func (s *Server) loadSession(w http.ResponseWriter, r *http.Request) (*store.Session, []*questionRow, bool) {
	sid, ok := pathID(w, r, "id")
	if !ok {
		return nil, nil, false
	}
	se, err := s.store.GetSession(sid)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return nil, nil, false
	}
	if err != nil {
		s.fail(w, err)
		return nil, nil, false
	}
	qs, err := s.store.Questions(sid)
	if err != nil {
		s.fail(w, err)
		return nil, nil, false
	}
	statuses, err := s.store.AnswerStatuses(sid)
	if err != nil {
		s.fail(w, err)
		return nil, nil, false
	}
	hasChild := map[int64]bool{}
	for _, q := range qs {
		hasChild[q.ParentID] = true
	}
	rows := make([]*questionRow, len(qs))
	for i, q := range qs {
		rows[i] = &questionRow{Question: q, Status: statuses[q.ID], IsGroup: hasChild[q.ID] && q.ID != 0}
	}
	return se, rows, true
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	se, rows, ok := s.loadSession(w, r)
	if !ok {
		return
	}
	docs, err := s.store.Documents(se.ID, "")
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "session", map[string]any{
		"Title":         se.Title,
		"Session":       se,
		"Questions":     rows,
		"Documents":     docs,
		"ContextWindow": s.cfg.LLMContextWindow,
	})
}

func (s *Server) handleQuestion(w http.ResponseWriter, r *http.Request) {
	s.questionPage(w, r, http.StatusOK, "")
}

func (s *Server) questionPage(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	se, rows, ok := s.loadSession(w, r)
	if !ok {
		return
	}
	qid, ok := pathID(w, r, "qid")
	if !ok {
		return
	}
	idx := -1
	for i, row := range rows {
		if row.ID == qid {
			idx = i
		}
	}
	if idx < 0 {
		http.NotFound(w, r)
		return
	}
	cur := rows[idx]

	// Previous/next move through answerable (non-group) questions.
	var prev, next *questionRow
	for i := idx - 1; i >= 0 && prev == nil; i-- {
		if !rows[i].IsGroup {
			prev = rows[i]
		}
	}
	for i := idx + 1; i < len(rows) && next == nil; i++ {
		if !rows[i].IsGroup {
			next = rows[i]
		}
	}

	var children []*questionRow
	for _, row := range rows {
		if row.ParentID == cur.ID {
			children = append(children, row)
		}
	}

	var ans *store.Answer
	if !cur.IsGroup {
		a, err := s.store.GetAnswer(cur.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.fail(w, err)
			return
		}
		ans = a
	}

	s.render(w, status, "question", map[string]any{
		"Title":    "Question " + cur.Number + " - " + se.Title,
		"Session":  se,
		"Question": cur,
		"Chain":    ancestors(rows, cur),
		"Children": children,
		"Answer":   ans,
		"Prev":     prev,
		"Next":     next,
		"Error":    errMsg,
		"Busy":     s.isBusy(cur.ID),
	})
}

func ancestors(rows []*questionRow, q *questionRow) []*questionRow {
	byID := map[int64]*questionRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	var chain []*questionRow
	for p := byID[q.ParentID]; p != nil && len(chain) < 10; p = byID[p.ParentID] {
		chain = append([]*questionRow{p}, chain...)
	}
	return chain
}

func (s *Server) isBusy(qid int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inFlight[qid]
}

func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	se, rows, ok := s.loadSession(w, r)
	if !ok {
		return
	}
	qid, ok := pathID(w, r, "qid")
	if !ok {
		return
	}
	var cur *questionRow
	for _, row := range rows {
		if row.ID == qid {
			cur = row
		}
	}
	if cur == nil || cur.IsGroup {
		http.NotFound(w, r)
		return
	}
	if s.llm == nil {
		s.questionPage(w, r, http.StatusServiceUnavailable, "The LLM is not configured: "+s.llmErr)
		return
	}

	s.mu.Lock()
	if s.inFlight[qid] {
		s.mu.Unlock()
		s.questionPage(w, r, http.StatusConflict, "An answer for this question is already being generated. Please wait and refresh.")
		return
	}
	s.inFlight[qid] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.inFlight, qid)
		s.mu.Unlock()
	}()

	refs, err := s.store.Documents(se.ID, "reference")
	if err != nil {
		s.fail(w, err)
		return
	}
	var chain []*store.Question
	for _, a := range ancestors(rows, cur) {
		chain = append(chain, a.Question)
	}
	ans, err := s.answer.Generate(r.Context(), answer.BuildReference(refs), chain, cur.Question)
	if err != nil {
		log.Printf("question %d: answer generation failed: %v", qid, err)
		s.questionPage(w, r, http.StatusBadGateway, "The answer could not be generated: "+err.Error())
		return
	}
	if err := s.store.SaveAnswer(ans); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/sessions/%d/questions/%d#answer", se.ID, qid), http.StatusSeeOther)
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}
