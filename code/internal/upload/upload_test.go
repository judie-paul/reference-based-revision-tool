package upload

import (
	"errors"
	"testing"

	"revisiontool/internal/testdocs"
)

func TestValidate(t *testing.T) {
	pdf := testdocs.PDF([][]testdocs.Line{{{Text: "hello"}}})
	docx := testdocs.DOCX([]testdocs.Para{{Text: "hello"}})

	cases := []struct {
		name    string
		file    string
		data    []byte
		max     int64
		want    Kind
		wantErr bool
	}{
		{"pdf ok", "a.pdf", pdf, 1 << 20, KindPDF, false},
		{"docx ok", "notes.DOCX", docx, 1 << 20, KindDOCX, false},
		{"too big", "a.pdf", pdf, 10, "", true},
		{"renamed text file", "a.pdf", []byte("just text"), 1 << 20, "", true},
		{"extension mismatch", "a.docx", pdf, 1 << 20, "", true},
		{"empty", "a.pdf", nil, 1 << 20, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k, err := Validate(c.file, c.data, c.max)
			if c.wantErr {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("expected ValidationError, got %v", err)
				}
				return
			}
			if err != nil || k != c.want {
				t.Fatalf("got %v, %v", k, err)
			}
		})
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Users\x\exam.pdf`: "exam.pdf",
		"../../etc/passwd":    "passwd",
		"":                    "document",
	} {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}
