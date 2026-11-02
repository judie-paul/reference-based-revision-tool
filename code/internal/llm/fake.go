package llm

import (
	"context"
	"fmt"
)

// Fake is a scripted Client for tests. Each call returns the next reply in
// order and records the request.
type Fake struct {
	Replies  []string
	Requests []Request
}

func (f *Fake) Model() string { return "fake/model" }

func (f *Fake) Complete(_ context.Context, req Request) (string, error) {
	f.Requests = append(f.Requests, req)
	if len(f.Requests) > len(f.Replies) {
		return "", fmt.Errorf("fake: no reply scripted for call %d", len(f.Requests))
	}
	return f.Replies[len(f.Requests)-1], nil
}
