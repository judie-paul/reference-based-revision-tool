// Package store persists revision sessions in SQLite (modernc.org/sqlite, a
// pure-Go driver, so no CGO is needed on Windows or Linux). All SQL lives in
// this package.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"revisiontool/internal/extract"
)

// Answer statuses.
const (
	StatusDocument     = "DOCUMENT-BASED"
	StatusSupplemented = "SUPPLEMENTED"
	StatusExternal     = "EXTERNAL"
	// StatusUnanswered records that no answer could be established from the
	// documents or external sources. It is not an answer status shown as a
	// source label; it tells the student nothing reliable was found.
	StatusUnanswered = "NOT ANSWERED"
)

// Segment bases: what supports a part of an answer.
const (
	BasisDocument    = "document"
	BasisExternal    = "external"
	BasisUnsupported = "unsupported"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

type Session struct {
	ID              int64
	Title           string
	CreatedAt       time.Time
	QuestionsStatus string // "pending", "ok", "failed"
	QuestionsError  string
	UnassignedText  []string // paper text not attributed to any extracted question
	ReferenceTokens int
	QuestionCount   int
	AnsweredCount   int
}

type Document struct {
	ID        int64
	SessionID int64
	Role      string // "paper" or "reference"
	Filename  string
	Kind      string
	SizeBytes int64
	Content   *extract.Document
}

type Question struct {
	ID        int64
	SessionID int64
	Seq       int
	Number    string // as printed, e.g. "1(a)"
	ParentID  int64  // 0 for top-level questions
	ParentSeq int    // used only when inserting: index of parent in the slice, -1 for none
	Text      string
	Marks     string
	// Verified is false when the extracted text could not be located in the
	// question paper, i.e. the extraction may be unreliable.
	Verified bool
	Depth    int
}

type Answer struct {
	ID             int64
	QuestionID     int64
	Status         string
	Notice         string
	MissingInfo    string
	Contradictions string
	Ambiguity      string
	Model          string
	CreatedAt      time.Time
	Segments       []Segment
}

type Segment struct {
	ID      int64
	Seq     int
	Text    string
	Basis   string
	Sources []Source
}

type Source struct {
	ID   int64
	Kind string // "document" or "external"

	// Document references.
	DocumentID int64
	Filename   string // joined from documents, not stored twice
	Page       int    // 0 when unknown
	PageApprox bool
	Section    string
	Quote      string
	Verified   bool

	// External references.
	Title   string
	URL     string
	Snippet string
}

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
	id INTEGER PRIMARY KEY,
	title TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL,
	questions_status TEXT NOT NULL DEFAULT 'pending',
	questions_error TEXT NOT NULL DEFAULT '',
	unassigned_text TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE IF NOT EXISTS documents (
	id INTEGER PRIMARY KEY,
	session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	role TEXT NOT NULL CHECK (role IN ('paper','reference')),
	filename TEXT NOT NULL,
	kind TEXT NOT NULL,
	size_bytes INTEGER NOT NULL,
	token_estimate INTEGER NOT NULL DEFAULT 0,
	pages_reliable INTEGER NOT NULL DEFAULT 1,
	content_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS questions (
	id INTEGER PRIMARY KEY,
	session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	seq INTEGER NOT NULL,
	number TEXT NOT NULL,
	parent_id INTEGER REFERENCES questions(id) ON DELETE CASCADE,
	text TEXT NOT NULL,
	marks TEXT NOT NULL DEFAULT '',
	verified INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS answers (
	id INTEGER PRIMARY KEY,
	question_id INTEGER NOT NULL UNIQUE REFERENCES questions(id) ON DELETE CASCADE,
	status TEXT NOT NULL,
	notice TEXT NOT NULL DEFAULT '',
	missing_info TEXT NOT NULL DEFAULT '',
	contradictions TEXT NOT NULL DEFAULT '',
	ambiguity TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL
);
CREATE TABLE IF NOT EXISTS answer_segments (
	id INTEGER PRIMARY KEY,
	answer_id INTEGER NOT NULL REFERENCES answers(id) ON DELETE CASCADE,
	seq INTEGER NOT NULL,
	text TEXT NOT NULL,
	basis TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS source_refs (
	id INTEGER PRIMARY KEY,
	segment_id INTEGER NOT NULL REFERENCES answer_segments(id) ON DELETE CASCADE,
	kind TEXT NOT NULL CHECK (kind IN ('document','external')),
	document_id INTEGER REFERENCES documents(id) ON DELETE CASCADE,
	page INTEGER NOT NULL DEFAULT 0,
	section TEXT NOT NULL DEFAULT '',
	quote TEXT NOT NULL DEFAULT '',
	verified INTEGER NOT NULL DEFAULT 0,
	title TEXT NOT NULL DEFAULT '',
	url TEXT NOT NULL DEFAULT '',
	snippet TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_documents_session ON documents(session_id);
CREATE INDEX IF NOT EXISTS idx_questions_session ON questions(session_id, seq);
CREATE INDEX IF NOT EXISTS idx_segments_answer ON answer_segments(answer_id, seq);
CREATE INDEX IF NOT EXISTS idx_sources_segment ON source_refs(segment_id);
`

// Open opens (and creates if needed) the database at path.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite allows one writer; keeps things simple and safe.
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// CreateSession stores a session together with its documents atomically.
func (s *Store) CreateSession(title string, docs []*Document, tokenEstimates []int) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO sessions (title, created_at) VALUES (?, ?)`, title, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	sid, _ := res.LastInsertId()
	for i, d := range docs {
		content, err := json.Marshal(d.Content)
		if err != nil {
			return 0, err
		}
		tokens := 0
		if i < len(tokenEstimates) {
			tokens = tokenEstimates[i]
		}
		res, err := tx.Exec(`INSERT INTO documents (session_id, role, filename, kind, size_bytes, token_estimate, pages_reliable, content_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, sid, d.Role, d.Filename, d.Kind, d.SizeBytes, tokens, d.Content.PageNumbersReliable, string(content))
		if err != nil {
			return 0, err
		}
		d.ID, _ = res.LastInsertId()
		d.SessionID = sid
	}
	return sid, tx.Commit()
}

const sessionCols = `s.id, s.title, s.created_at, s.questions_status, s.questions_error, s.unassigned_text,
	(SELECT COALESCE(SUM(token_estimate),0) FROM documents WHERE session_id = s.id AND role = 'reference'),
	(SELECT COUNT(*) FROM questions WHERE session_id = s.id),
	(SELECT COUNT(*) FROM answers a JOIN questions q ON q.id = a.question_id WHERE q.session_id = s.id)`

func scanSession(sc interface{ Scan(...any) error }) (*Session, error) {
	var se Session
	var unassigned string
	if err := sc.Scan(&se.ID, &se.Title, &se.CreatedAt, &se.QuestionsStatus, &se.QuestionsError, &unassigned,
		&se.ReferenceTokens, &se.QuestionCount, &se.AnsweredCount); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(unassigned), &se.UnassignedText)
	return &se, nil
}

func (s *Store) GetSession(id int64) (*Session, error) {
	se, err := scanSession(s.db.QueryRow(`SELECT `+sessionCols+` FROM sessions s WHERE s.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return se, err
}

func (s *Store) ListSessions() ([]*Session, error) {
	rows, err := s.db.Query(`SELECT ` + sessionCols + ` FROM sessions s ORDER BY s.created_at DESC, s.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		se, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, se)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSession(id int64) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// Documents returns a session's documents, paper first, then references in
// upload order. role filters when non-empty.
func (s *Store) Documents(sessionID int64, role string) ([]*Document, error) {
	rows, err := s.db.Query(`SELECT id, session_id, role, filename, kind, size_bytes, content_json FROM documents
		WHERE session_id = ? AND (? = '' OR role = ?) ORDER BY role = 'reference', id`, sessionID, role, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Document
	for rows.Next() {
		var d Document
		var content string
		if err := rows.Scan(&d.ID, &d.SessionID, &d.Role, &d.Filename, &d.Kind, &d.SizeBytes, &content); err != nil {
			return nil, err
		}
		d.Content = &extract.Document{}
		if err := json.Unmarshal([]byte(content), d.Content); err != nil {
			return nil, fmt.Errorf("decoding document %d: %w", d.ID, err)
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// SetQuestions replaces the session's questions and records the extraction
// outcome. Question.ParentSeq refers to the index of the parent in qs.
func (s *Store) SetQuestions(sessionID int64, qs []Question, unassigned []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM questions WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	ids := make([]int64, len(qs))
	for i, q := range qs {
		var parent any
		if q.ParentSeq >= 0 && q.ParentSeq < i {
			parent = ids[q.ParentSeq]
		}
		res, err := tx.Exec(`INSERT INTO questions (session_id, seq, number, parent_id, text, marks, verified)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, sessionID, i, q.Number, parent, q.Text, q.Marks, q.Verified)
		if err != nil {
			return err
		}
		ids[i], _ = res.LastInsertId()
	}
	if unassigned == nil {
		unassigned = []string{}
	}
	ua, _ := json.Marshal(unassigned)
	if _, err := tx.Exec(`UPDATE sessions SET questions_status = 'ok', questions_error = '', unassigned_text = ? WHERE id = ?`,
		string(ua), sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetQuestionsFailed records that question extraction failed.
func (s *Store) SetQuestionsFailed(sessionID int64, msg string) error {
	_, err := s.db.Exec(`UPDATE sessions SET questions_status = 'failed', questions_error = ? WHERE id = ?`, msg, sessionID)
	return err
}

// Questions returns the session's questions in paper order with Depth set.
func (s *Store) Questions(sessionID int64) ([]*Question, error) {
	rows, err := s.db.Query(`SELECT id, session_id, seq, number, COALESCE(parent_id, 0), text, marks, verified
		FROM questions WHERE session_id = ? ORDER BY seq`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Question
	depth := map[int64]int{}
	for rows.Next() {
		var q Question
		if err := rows.Scan(&q.ID, &q.SessionID, &q.Seq, &q.Number, &q.ParentID, &q.Text, &q.Marks, &q.Verified); err != nil {
			return nil, err
		}
		if q.ParentID != 0 {
			q.Depth = depth[q.ParentID] + 1
		}
		depth[q.ID] = q.Depth
		out = append(out, &q)
	}
	return out, rows.Err()
}

// AnswerStatuses maps question ID to answer status for a session.
func (s *Store) AnswerStatuses(sessionID int64) (map[int64]string, error) {
	rows, err := s.db.Query(`SELECT a.question_id, a.status FROM answers a JOIN questions q ON q.id = a.question_id
		WHERE q.session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var st string
		if err := rows.Scan(&id, &st); err != nil {
			return nil, err
		}
		out[id] = st
	}
	return out, rows.Err()
}

// SaveAnswer replaces any existing answer for the question.
func (s *Store) SaveAnswer(a *Answer) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM answers WHERE question_id = ?`, a.QuestionID); err != nil {
		return err
	}
	a.CreatedAt = time.Now().UTC()
	res, err := tx.Exec(`INSERT INTO answers (question_id, status, notice, missing_info, contradictions, ambiguity, model, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, a.QuestionID, a.Status, a.Notice, a.MissingInfo, a.Contradictions, a.Ambiguity, a.Model, a.CreatedAt)
	if err != nil {
		return err
	}
	a.ID, _ = res.LastInsertId()
	for i := range a.Segments {
		seg := &a.Segments[i]
		seg.Seq = i
		res, err := tx.Exec(`INSERT INTO answer_segments (answer_id, seq, text, basis) VALUES (?, ?, ?, ?)`, a.ID, i, seg.Text, seg.Basis)
		if err != nil {
			return err
		}
		seg.ID, _ = res.LastInsertId()
		for j := range seg.Sources {
			src := &seg.Sources[j]
			var docID any
			if src.Kind == "document" {
				docID = src.DocumentID
			}
			res, err := tx.Exec(`INSERT INTO source_refs (segment_id, kind, document_id, page, section, quote, verified, title, url, snippet)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, seg.ID, src.Kind, docID, src.Page, src.Section, src.Quote, src.Verified, src.Title, src.URL, src.Snippet)
			if err != nil {
				return err
			}
			src.ID, _ = res.LastInsertId()
		}
	}
	return tx.Commit()
}

// GetAnswer loads the answer for a question with its segments and sources.
func (s *Store) GetAnswer(questionID int64) (*Answer, error) {
	var a Answer
	err := s.db.QueryRow(`SELECT id, question_id, status, notice, missing_info, contradictions, ambiguity, model, created_at
		FROM answers WHERE question_id = ?`, questionID).
		Scan(&a.ID, &a.QuestionID, &a.Status, &a.Notice, &a.MissingInfo, &a.Contradictions, &a.Ambiguity, &a.Model, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`SELECT id, seq, text, basis FROM answer_segments WHERE answer_id = ? ORDER BY seq`, a.ID)
	if err != nil {
		return nil, err
	}
	index := map[int64]int{}
	for rows.Next() {
		var seg Segment
		if err := rows.Scan(&seg.ID, &seg.Seq, &seg.Text, &seg.Basis); err != nil {
			rows.Close()
			return nil, err
		}
		index[seg.ID] = len(a.Segments)
		a.Segments = append(a.Segments, seg)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT r.id, r.segment_id, r.kind, COALESCE(r.document_id, 0), COALESCE(d.filename, ''),
			COALESCE(d.pages_reliable, 1), r.page, r.section, r.quote, r.verified, r.title, r.url, r.snippet
		FROM source_refs r
		JOIN answer_segments g ON g.id = r.segment_id
		LEFT JOIN documents d ON d.id = r.document_id
		WHERE g.answer_id = ? ORDER BY r.id`, a.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var src Source
		var segID int64
		var reliable bool
		if err := rows.Scan(&src.ID, &segID, &src.Kind, &src.DocumentID, &src.Filename, &reliable, &src.Page,
			&src.Section, &src.Quote, &src.Verified, &src.Title, &src.URL, &src.Snippet); err != nil {
			return nil, err
		}
		src.PageApprox = src.DocumentID != 0 && !reliable
		if i, ok := index[segID]; ok {
			a.Segments[i].Sources = append(a.Segments[i].Sources, src)
		}
	}
	return &a, rows.Err()
}
