// Package upload validates uploaded files: size limits and real file type
// detection based on content (magic bytes / container structure), not on the
// filename extension.
package upload

import (
	"archive/zip"
	"bytes"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// Kind is the detected type of an uploaded document.
type Kind string

const (
	KindPDF  Kind = "pdf"
	KindDOCX Kind = "docx"
)

// ValidationError is a user-facing upload error.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(format, a...)} }

// Detect inspects the file content and returns its kind.
func Detect(data []byte) (Kind, error) {
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		return KindPDF, nil
	}
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", invalid("the file looks like a ZIP archive but could not be opened")
		}
		for _, f := range zr.File {
			if f.Name == "word/document.xml" {
				return KindDOCX, nil
			}
		}
		return "", invalid("the file is a ZIP archive but not a Word (.docx) document")
	}
	return "", invalid("unsupported file type: only PDF and DOCX documents are accepted")
}

// Validate checks size and type of one file. maxBytes is the configured limit.
func Validate(filename string, data []byte, maxBytes int64) (Kind, error) {
	name := SafeName(filename)
	if len(data) == 0 {
		return "", invalid("%s is empty", name)
	}
	if int64(len(data)) > maxBytes {
		return "", invalid("%s is %s, which exceeds the %s limit", name, HumanSize(int64(len(data))), HumanSize(maxBytes))
	}
	kind, err := Detect(data)
	if err != nil {
		return "", invalid("%s: %v", name, err)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != "" && ext != "."+string(kind) {
		return "", invalid("%s: the file extension %q does not match its content (%s)", name, ext, strings.ToUpper(string(kind)))
	}
	return kind, nil
}

// SafeName strips any directory components from a client-supplied filename so
// it can be displayed and stored without exposing or trusting paths.
func SafeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base("/" + name)
	name = strings.TrimSpace(name)
	if name == "" || name == "/" || name == "." {
		return "document"
	}
	if len(name) > 200 {
		name = name[:200]
	}
	return name
}

// HumanSize formats a byte count, e.g. "10 MB".
func HumanSize(n int64) string {
	switch {
	case n >= 1<<20:
		v := float64(n) / (1 << 20)
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d MB", int64(v))
		}
		return fmt.Sprintf("%.1f MB", v)
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}
