// Package auth turns a bearer token into the subject the gateway authorises.
//
// It authenticates and does nothing else. A token that verifies yields a
// subject, and what that subject may do is MCPGrant's business — the scopes in
// the token are passed along for the record and grant nothing here.
//
// Two ways of verifying a token are supported, because OAuth providers issue
// both kinds and the gateway must not care which one it is behind:
//
//   - A JWT is verified locally against the provider's JWKS.
//   - An opaque token is sent to the provider's RFC 7662 introspection
//     endpoint. Ory Hydra issues these by default.
//
// Either may be configured, or both. With both, a token that looks like a JWT is
// verified as one and everything else is introspected.
//
// Nothing here is specific to a provider. The claims read are the registered
// ones (RFC 7519, RFC 7662), and the one that names the subject is
// configurable.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"

	"forgejo.metapatrol.local/platform/mcp-gate/internal/metrics"
)

// Config configures a Verifier.
type Config struct {
	// Issuer is the expected iss claim. Empty skips the check, which is only
	// sensible when the JWKS or introspection endpoint already belongs to one
	// issuer and nothing else can mint tokens it accepts.
	Issuer string

	// Audience is the expected aud claim — the gateway's own resource
	// identifier. Empty skips the check. Setting it keeps a token issued for
	// some other service from being replayed here.
	Audience string

	// JWKSURL is where the provider publishes the keys JWTs are signed with.
	JWKSURL string

	// IntrospectionURL is the provider's RFC 7662 introspection endpoint.
	IntrospectionURL string
	// IntrospectionClientID and IntrospectionClientSecret authenticate the
	// gateway to the introspection endpoint with HTTP Basic, as RFC 7662
	// expects. Leave both empty for an endpoint that is protected by the
	// network instead, as Hydra's admin port usually is.
	IntrospectionClientID     string
	IntrospectionClientSecret string
	// IntrospectionCacheTTL bounds how long an introspection answer is reused.
	// A revoked token stays usable for at most this long. Defaults to 30s.
	IntrospectionCacheTTL time.Duration

	// SubjectClaim names the claim the subject is read from. Defaults to
	// "sub", which for the client credentials grant is the client ID. A path
	// such as "ext.subject" reads a nested claim.
	SubjectClaim string

	// ClockSkew is tolerated on exp and nbf. Defaults to 30s.
	ClockSkew time.Duration

	// HTTPClient carries requests to the JWKS and introspection endpoints.
	// Defaults to a client with a 10s timeout.
	HTTPClient *http.Client
}

// Validate checks that the configuration can verify anything at all.
func (c Config) Validate() error {
	if c.JWKSURL == "" && c.IntrospectionURL == "" {
		return errors.New("auth: at least one of a JWKS URL or an introspection URL is required")
	}
	if (c.IntrospectionClientID == "") != (c.IntrospectionClientSecret == "") {
		return errors.New("auth: the introspection client ID and secret go together")
	}
	return nil
}

func (c Config) withDefaults() Config {
	if c.SubjectClaim == "" {
		c.SubjectClaim = "sub"
	}
	if c.ClockSkew <= 0 {
		c.ClockSkew = 30 * time.Second
	}
	if c.IntrospectionCacheTTL <= 0 {
		c.IntrospectionCacheTTL = 30 * time.Second
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	return c
}

// Verifier verifies bearer tokens. It is safe for concurrent use.
type Verifier struct {
	cfg          Config
	keys         *keySet
	introspector *introspector
}

// NewVerifier builds a Verifier. It contacts nothing: keys are fetched on the
// first token, so a gateway starts while its OAuth provider is still coming up
// and reports the provider's absence per request instead of crash-looping.
func NewVerifier(cfg Config) (*Verifier, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.withDefaults()
	v := &Verifier{cfg: cfg}
	if cfg.JWKSURL != "" {
		v.keys = newKeySet(cfg.JWKSURL, cfg.HTTPClient)
	}
	if cfg.IntrospectionURL != "" {
		v.introspector = newIntrospector(cfg)
	}
	return v, nil
}

// Failure reasons, as they appear in mcp_gate_auth_failures_total.
const (
	ReasonMissingToken = "missing_token"
	ReasonMalformed    = "malformed"
	ReasonSignature    = "signature"
	ReasonExpired      = "expired"
	ReasonNotYetValid  = "not_yet_valid"
	ReasonNoExpiration = "no_expiration"
	ReasonIssuer       = "issuer"
	ReasonAudience     = "audience"
	ReasonInactive     = "inactive"
	ReasonNoSubject    = "no_subject"
	ReasonUnsupported  = "unsupported"
	ReasonUnavailable  = "unavailable"
)

// rejection is a refused token. It unwraps to the SDK's ErrInvalidToken, which
// is what makes its middleware answer 401 with a WWW-Authenticate challenge.
type rejection struct{ reason string }

func (r *rejection) Error() string { return "invalid token: " + strings.ReplaceAll(r.reason, "_", " ") }
func (r *rejection) Unwrap() error { return sdkauth.ErrInvalidToken }

func reject(reason string) error { return &rejection{reason: reason} }

// unavailable is a token that could not be checked because the provider could
// not be asked. It is deliberately not ErrInvalidToken: the token may be fine,
// and a 401 would send a well-behaved client off to fetch a new one from the
// same provider that is down.
type unavailable struct{ err error }

func (u *unavailable) Error() string { return "token verification is unavailable" }
func (u *unavailable) Unwrap() error { return u.err }

// Verify implements the SDK's TokenVerifier.
func (v *Verifier) Verify(ctx context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
	info, err := v.verify(ctx, token)
	if err != nil {
		var r *rejection
		switch {
		case errors.As(err, &r):
			metrics.RecordAuthFailure(r.reason)
		default:
			metrics.RecordAuthFailure(ReasonUnavailable)
		}
		return nil, err
	}
	return info, nil
}

func (v *Verifier) verify(ctx context.Context, token string) (*sdkauth.TokenInfo, error) {
	if looksLikeJWT(token) && v.keys != nil {
		return v.verifyJWT(ctx, token)
	}
	if v.introspector != nil {
		return v.introspector.introspect(ctx, token)
	}
	// A JWT-shaped token with only introspection configured goes to the
	// introspector above; this is an opaque token with only a JWKS, which
	// nothing here can check.
	return nil, reject(ReasonUnsupported)
}

// looksLikeJWT reports whether the token has the three dot-separated segments of
// a compact JWS.
func looksLikeJWT(token string) bool {
	parts := strings.Split(token, ".")
	return len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != ""
}

// tokenInfo builds the SDK's view of a verified token from its claims.
//
// UserID is the subject. The SDK binds a session to it, so a session opened
// with one client's token cannot be continued with another's.
func tokenInfo(cfg Config, claims map[string]any, expiration time.Time) (*sdkauth.TokenInfo, error) {
	subject, ok := stringClaim(claims, cfg.SubjectClaim)
	if !ok {
		return nil, reject(ReasonNoSubject)
	}
	info := &sdkauth.TokenInfo{
		UserID:     subject,
		Scopes:     scopes(claims),
		Expiration: expiration,
		Extra:      map[string]any{},
	}
	if clientID, ok := stringClaim(claims, "client_id"); ok {
		info.Extra["client_id"] = clientID
	}
	return info, nil
}

// stringClaim reads a non-empty string claim. A name that exists as a top-level
// claim is read as it is, even when it contains dots — claims named by URL are
// common — and only otherwise is it followed as a path into nested objects.
func stringClaim(claims map[string]any, name string) (string, bool) {
	if v, ok := claims[name]; ok {
		s, ok := v.(string)
		return s, ok && s != ""
	}
	var current any = claims
	for _, part := range strings.Split(name, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		if current, ok = m[part]; !ok {
			return "", false
		}
	}
	s, ok := current.(string)
	return s, ok && s != ""
}

// scopes reads the granted scopes: RFC 8693's space-separated "scope" string,
// or the "scp" array some providers use instead.
func scopes(claims map[string]any) []string {
	if s, ok := claims["scope"].(string); ok && s != "" {
		return strings.Fields(s)
	}
	if list, ok := claims["scp"].([]any); ok {
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// audienceMatches reports whether aud — a string or a list of strings, as RFC
// 7519 allows — contains want.
func audienceMatches(aud any, want string) bool {
	switch aud := aud.(type) {
	case string:
		return aud == want
	case []any:
		for _, item := range aud {
			if s, ok := item.(string); ok && s == want {
				return true
			}
		}
	case []string:
		for _, s := range aud {
			if s == want {
				return true
			}
		}
	}
	return false
}

// String describes the configuration for the startup log, without secrets.
func (v *Verifier) String() string {
	var methods []string
	if v.keys != nil {
		methods = append(methods, "jwks="+v.cfg.JWKSURL)
	}
	if v.introspector != nil {
		methods = append(methods, "introspection="+v.cfg.IntrospectionURL)
	}
	return fmt.Sprintf("%s issuer=%q audience=%q subjectClaim=%q",
		strings.Join(methods, " "), v.cfg.Issuer, v.cfg.Audience, v.cfg.SubjectClaim)
}
