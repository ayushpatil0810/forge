// Package mock is a scripted llm.Provider for deterministic tests and demos.
package mock

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/ayushpatil0810/forge/internal/llm"
)

// Turn is the scripted response for one model call.
type Turn []llm.StreamEvent

func Text(s string) llm.StreamEvent { return llm.StreamEvent{Type: llm.StreamText, Text: s} }

func ToolUse(id, name string, input any) llm.StreamEvent {
	b, _ := json.Marshal(input)
	return llm.StreamEvent{Type: llm.StreamToolCall, ToolCall: &llm.ToolCall{ID: id, Name: name, Input: b}}
}

type Provider struct {
	mu       sync.Mutex
	turns    []Turn
	next     int
	Requests []llm.Request // every request received, for assertions
}

func New(turns ...Turn) *Provider { return &Provider{turns: turns} }

func (p *Provider) Name() string { return "mock" }

func (p *Provider) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	p.mu.Lock()
	p.Requests = append(p.Requests, req)
	if p.next >= len(p.turns) {
		p.mu.Unlock()
		return nil, errors.New("mock: no more scripted turns")
	}
	turn := p.turns[p.next]
	p.next++
	p.mu.Unlock()

	ch := make(chan llm.StreamEvent)
	go func() {
		defer close(ch)
		for _, ev := range turn {
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
		select {
		case ch <- llm.StreamEvent{Type: llm.StreamDone}:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}
