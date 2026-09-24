package ui

import "context"

type tokenKey struct{}

// withToken carries the token read for one request to the proxy's Rewrite,
// which has no way to fail: the token is read, and a failure answered, before
// the request is handed to the proxy.
func withToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey{}, token)
}
