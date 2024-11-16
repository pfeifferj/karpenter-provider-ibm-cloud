package options

import (
	"context"
)

// Key is the context key for Options
type Key struct{}

// Options contains operator configuration
type Options struct {
	// Interruption enables the interruption controller
	Interruption bool
}

// FromContext retrieves Options from the context
func FromContext(ctx context.Context) Options {
	if v := ctx.Value(Key{}); v != nil {
		return v.(Options)
	}
	return Options{}
}

// WithOptions returns a new context with the given Options
func WithOptions(ctx context.Context, options Options) context.Context {
	return context.WithValue(ctx, Key{}, options)
}
