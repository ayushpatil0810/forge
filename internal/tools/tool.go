// Package tools holds the capabilities the agent can invoke.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ayushpatil0810/forge/internal/llm"
)

// Result is what the model sees after a tool runs.
type Result struct {
	Output  string `json:"output"`
	IsError bool   `json:"isError,omitempty"`
}

// ApprovalRequest details user approval requirements from SPEC §6.3.
// Zero value means no approval needed.
type ApprovalRequest struct {
	Description string `json:"description,omitempty"`
	Diff        string `json:"diff,omitempty"`
	IsProtected bool   `json:"isProtected,omitempty"`
	IsDangerous bool   `json:"isDangerous,omitempty"`
	Command     string `json:"command,omitempty"`
}

func (a ApprovalRequest) Required() bool {
	return a.Description != "" || a.Diff != "" || a.IsProtected || a.IsDangerous || a.Command != ""
}

// Tool abstraction from SPEC §6.3.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Run(ctx context.Context, input json.RawMessage) (Result, error)
	NeedsApproval(input json.RawMessage) ApprovalRequest // zero value means none
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry(ts ...Tool) *Registry {
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range ts {
		r.Register(t)
	}
	return r
}

func (r *Registry) Register(t Tool) {
	name := t.Name()
	if _, dup := r.tools[name]; dup {
		panic(fmt.Sprintf("tools: duplicate tool %q", name))
	}
	r.tools[name] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Specs returns tool specs in stable order for LLM requests.
func (r *Registry) Specs() []llm.ToolSpec {
	specs := make([]llm.ToolSpec, 0, len(r.tools))
	for _, t := range r.tools {
		specs = append(specs, llm.ToolSpec{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
		})
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}
