// Package tools holds the capabilities the agent can invoke.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ayushpatil0810/forge/internal/llm"
)

// Risk drives the permission policy.
type Risk string

const (
	RiskRead  Risk = "read"  // no side effects
	RiskWrite Risk = "write" // modifies files in the workspace
	RiskExec  Risk = "exec"  // runs commands / touches the network
)

// Result is what the model sees after a tool runs.
type Result struct {
	Output  string
	IsError bool
}

type Tool interface {
	Spec() llm.ToolSpec
	// Risk may depend on input (e.g. `bash` with `ls` vs `rm -rf`).
	Risk(input json.RawMessage) Risk
	Run(ctx context.Context, input json.RawMessage) (Result, error)
}

type Registry struct{ tools map[string]Tool }

func NewRegistry(ts ...Tool) *Registry {
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range ts {
		r.Register(t)
	}
	return r
}

func (r *Registry) Register(t Tool) {
	name := t.Spec().Name
	if _, dup := r.tools[name]; dup {
		panic(fmt.Sprintf("tools: duplicate tool %q", name))
	}
	r.tools[name] = t
}

func (r *Registry) Get(name string) (Tool, bool) { t, ok := r.tools[name]; return t, ok }

// Specs returns tool specs in stable order (keeps prompts cache-friendly).
func (r *Registry) Specs() []llm.ToolSpec {
	specs := make([]llm.ToolSpec, 0, len(r.tools))
	for _, t := range r.tools {
		specs = append(specs, t.Spec())
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}
