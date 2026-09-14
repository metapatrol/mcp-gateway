package auth

import (
	"context"
	"testing"
)

// BenchmarkVerify_JWT is the token work of every request: an RS256 signature
// check against a cached key set, and the claim checks.
func BenchmarkVerify_JWT(b *testing.B) {
	t := &testing.T{}
	i := newIssuer(t)
	defer i.jwks.Close()
	v := i.verifier(t, nil)
	token := i.token(nil)
	if _, err := v.Verify(context.Background(), token, nil); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := v.Verify(context.Background(), token, nil); err != nil {
			b.Fatal(err)
		}
	}
}
