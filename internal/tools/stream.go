package tools

import "context"

// StreamFunc represents an incremental tool output chunk consumer.
type StreamFunc func(chunk string)

type streamContextKey struct{}

// ContextWithStream wraps a context with a streaming output callback.
func ContextWithStream(ctx context.Context, fn StreamFunc) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, streamContextKey{}, fn)
}

// StreamFromContext retrieves the StreamFunc from ctx, or returns a no-op if nil.
func StreamFromContext(ctx context.Context) StreamFunc {
	if fn, ok := ctx.Value(streamContextKey{}).(StreamFunc); ok && fn != nil {
		return fn
	}
	return func(string) {}
}

