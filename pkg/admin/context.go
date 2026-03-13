package admin

import "context"

type contextKey struct{}

// withCallerID stores the authenticated caller ID in the context.
func withCallerID(ctx context.Context, callerID string) context.Context {
	return context.WithValue(ctx, contextKey{}, callerID)
}

// CallerID extracts the authenticated caller ID from the context.
// Returns empty string if no caller ID is present (i.e., the request
// did not pass through the admin auth middleware).
func CallerID(ctx context.Context) string {
	v, _ := ctx.Value(contextKey{}).(string)
	return v
}
