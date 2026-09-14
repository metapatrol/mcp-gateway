package auth

import (
	"context"
	"testing"
)

// BenchmarkVerify_JWT is the token work of every request.
//
// "signature" is a token seen for the first time: an RS256 signature check
// against the cached key set, and the claim checks. "remembered" is the same
// token on every later request of its lifetime.
func BenchmarkVerify_JWT(b *testing.B) {
	t := &testing.T{}
	i := newIssuer(t)
	defer i.jwks.Close()
	v := i.verifier(t, nil)
	token := i.token(nil)
	if _, err := v.Verify(context.Background(), token, nil); err != nil {
		b.Fatal(err)
	}

	b.Run("signature", func(b *testing.B) {
		set, generation, err := v.keys.get(context.Background())
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if _, _, _, err := v.checkJWT(context.Background(), token, set, generation); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("remembered", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := v.Verify(context.Background(), token, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
}
