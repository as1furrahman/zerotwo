package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"zerotwo/pkg/provider"
)

type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	Run(argsJSON string) (string, error)
}

// ContextTool is an optional interface for tools that support graceful context cancellation.
type ContextTool interface {
	Tool
	RunWithContext(ctx context.Context, argsJSON string) (string, error)
}

type Registry struct {
	tools map[string]Tool
	order []string
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
	r.order = append(r.order, t.Name())
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) AsProviderTools() []provider.Tool {
	out := make([]provider.Tool, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		out = append(out, provider.Tool{
			Type: "function",
			Function: provider.ToolFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return out
}

func (r *Registry) Dispatch(name, argsJSON string) string {
	return r.DispatchContext(context.Background(), name, argsJSON)
}

func (r *Registry) DispatchContext(ctx context.Context, name, argsJSON string) string {
	t, ok := r.tools[name]
	if !ok {
		return fmt.Sprintf("ERROR: unknown tool %q. Available: %v", name, r.order)
	}
	var result string
	var err error
	if ct, ok := t.(ContextTool); ok {
		result, err = ct.RunWithContext(ctx, argsJSON)
	} else {
		result, err = t.Run(argsJSON)
	}
	if err != nil {
		return fmt.Sprintf("ERROR running %s: %v", name, err)
	}
	return result
}

func (r *Registry) Names() []string { return r.order }

// TruncateOutput caps output to maxBytes and appends an explicit marker.
func TruncateOutput(output string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	if len(output) > maxBytes {
		marker := fmt.Sprintf("\n[... truncated: output exceeded %d bytes ...]", maxBytes)
		cutPoint := maxBytes - len(marker)
		if cutPoint < 0 {
			cutPoint = 0
		}
		return output[:cutPoint] + marker
	}
	return output
}
