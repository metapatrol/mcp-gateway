// Package credentials authenticates the gateway's and the controller's requests
// to MCP backends, as an MCPServer's spec.auth says.
//
// It produces headers and nothing else. Which requests carry which credential,
// and what a refusal does to a session, is the discovery package's business.
package credentials

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/oauth"
)

// Credential authenticates requests.
type Credential interface {
	// Authorize sets the credential on the header of an outgoing request.
	Authorize(ctx context.Context, header http.Header) error

	// Refused tells the credential that the backend answered 401 to a request
	// authorised with header. It reports whether authorising again could send
	// something different, and so whether one more attempt is worth making.
	Refused(header http.Header) bool

	// String says what the credential is, for error messages. It never
	// includes a secret.
	String() string
}

// Auth is how one backend is authenticated.
type Auth struct {
	// Type is spec.auth.type, None when auth is left out.
	Type gatev1alpha1.BackendAuthType

	// Credential authenticates the requests made on the gateway's own behalf:
	// every request, or under Passthrough the catalog reads alone. Nil sends
	// none.
	Credential Credential

	// Fingerprint identifies the spec this was built from. Equal fingerprints
	// mean equal configuration; the Secret values themselves are read as they
	// are needed and are not part of it.
	Fingerprint string
}

// Passthrough reports whether tool calls carry the caller's own token.
func (a Auth) Passthrough() bool { return a.Type == gatev1alpha1.BackendAuthPassthrough }

// Build turns spec.auth into an Auth. It reads nothing: a missing Secret shows
// up when a request needs it, as an error that names it.
//
// A spec that does not hold together — which the CRD's validation rules keep
// out of the API server — yields a credential that fails every request with the
// reason, rather than an error here, so that it is reported where discovery
// failures are. So does a nil secrets, once a credential needs a Secret.
func Build(spec *gatev1alpha1.BackendAuth, secrets Secrets, httpClient *http.Client) Auth {
	if spec == nil || spec.Type == "" || spec.Type == gatev1alpha1.BackendAuthNone {
		// One fingerprint for every way of saying None: auth left out, and the
		// auth: {} the API server stores as type None, are the same backend.
		return Auth{Type: gatev1alpha1.BackendAuthNone}
	}
	// The credentials keep what they are built from, and the caller's object
	// may be one a cache hands out.
	spec = spec.DeepCopy()
	auth := Auth{Type: spec.Type, Fingerprint: fingerprint(spec)}
	if secrets == nil {
		secrets = noSecrets{}
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	switch spec.Type {
	case gatev1alpha1.BackendAuthPassthrough:
		if spec.Passthrough != nil && spec.Passthrough.Discovery != nil {
			d := spec.Passthrough.Discovery
			switch {
			case d.ClientCredentials != nil && d.APIKey == nil:
				auth.Credential = newClientCredentials(d.ClientCredentials, secrets, httpClient)
			case d.APIKey != nil && d.ClientCredentials == nil:
				auth.Credential = newAPIKey(d.APIKey, secrets)
			default:
				auth.Credential = failing{errors.New("passthrough.discovery must set exactly one of clientCredentials or apiKey")}
			}
		}
	case gatev1alpha1.BackendAuthClientCredentials:
		if spec.ClientCredentials == nil {
			auth.Credential = failing{errors.New("clientCredentials is required with type ClientCredentials")}
			break
		}
		auth.Credential = newClientCredentials(spec.ClientCredentials, secrets, httpClient)
	case gatev1alpha1.BackendAuthAPIKey:
		if spec.APIKey == nil {
			auth.Credential = failing{errors.New("apiKey is required with type APIKey")}
			break
		}
		auth.Credential = newAPIKey(spec.APIKey, secrets)
	default:
		auth.Credential = failing{fmt.Errorf("type %q is not supported", spec.Type)}
	}
	return auth
}

func fingerprint(spec *gatev1alpha1.BackendAuth) string {
	b, err := json.Marshal(spec)
	if err != nil {
		// A struct of strings and slices always encodes.
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Forward is the credential of a Passthrough tool call: the caller's
// Authorization header, sent on exactly as the caller presented it.
func Forward(authorization string) Credential { return forward(authorization) }

type forward string

func (f forward) Authorize(_ context.Context, header http.Header) error {
	header.Set("Authorization", string(f))
	return nil
}

// Refused is false: the token is the caller's, and there is no other one to try.
func (forward) Refused(http.Header) bool { return false }

func (forward) String() string { return "the caller's token" }

// apiKey sends a key from a Secret in a header.
type apiKey struct {
	secrets Secrets
	ref     gatev1alpha1.SecretKeyRef
	header  string
	prefix  string
}

func newAPIKey(spec *gatev1alpha1.APIKeyAuth, secrets Secrets) *apiKey {
	header := spec.Header
	if header == "" {
		header = "Authorization"
	}
	prefix := ""
	switch {
	case spec.Prefix != nil:
		prefix = *spec.Prefix
	case strings.EqualFold(header, "Authorization"):
		prefix = "Bearer "
	}
	return &apiKey{secrets: secrets, ref: spec.SecretRef, header: header, prefix: prefix}
}

func (k *apiKey) Authorize(ctx context.Context, header http.Header) error {
	key, err := k.secrets.Value(ctx, k.ref)
	if err != nil {
		return err
	}
	header.Set(k.header, k.prefix+key)
	return nil
}

// Refused reads the Secret again: the key may have been rotated since it was
// read, on both ends.
func (k *apiKey) Refused(http.Header) bool { return k.secrets.Invalidate(k.ref) }

func (k *apiKey) String() string {
	return fmt.Sprintf("the API key from secret %q, key %q", k.ref.Name, k.ref.Key)
}

// clientCredentials sends a token obtained with the client credentials grant.
type clientCredentials struct {
	spec    *gatev1alpha1.ClientCredentialsAuth
	secrets Secrets
	tokens  *oauth.Cache
}

func newClientCredentials(spec *gatev1alpha1.ClientCredentialsAuth, secrets Secrets, httpClient *http.Client) *clientCredentials {
	c := &clientCredentials{spec: spec, secrets: secrets}
	c.tokens = oauth.NewCache(func(ctx context.Context) (*oauth2.Token, error) {
		cfg, err := c.config(ctx)
		if err != nil {
			return nil, err
		}
		token, err := oauth.ClientCredentials(cfg, httpClient)(ctx)
		if err != nil {
			return nil, fmt.Errorf("obtaining a token from %s: %w", spec.TokenURL, err)
		}
		return token, nil
	})
	return c
}

// config reads the client's credentials for one token request. They are read
// every time a token is fetched, so a rotated client secret takes effect with
// the next token.
func (c *clientCredentials) config(ctx context.Context) (*clientcredentials.Config, error) {
	clientID := c.spec.ClientID
	if c.spec.ClientIDRef != nil {
		var err error
		if clientID, err = c.secrets.Value(ctx, *c.spec.ClientIDRef); err != nil {
			return nil, err
		}
	}
	if clientID == "" {
		return nil, errors.New("set clientID or clientIDRef")
	}
	clientSecret, err := c.secrets.Value(ctx, c.spec.ClientSecretRef)
	if err != nil {
		return nil, err
	}
	cfg := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     c.spec.TokenURL,
		Scopes:       c.spec.Scopes,
	}
	if len(c.spec.Audience) > 0 {
		cfg.EndpointParams = url.Values{"audience": {strings.Join(c.spec.Audience, " ")}}
	}
	return cfg, nil
}

func (c *clientCredentials) Authorize(ctx context.Context, header http.Header) error {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return err
	}
	header.Set("Authorization", "Bearer "+token.AccessToken)
	return nil
}

// Refused drops the token. It may have been revoked, or the backend's clock
// may put it past its expiry already; a new one is worth one attempt.
func (c *clientCredentials) Refused(header http.Header) bool {
	c.tokens.Invalidate(strings.TrimPrefix(header.Get("Authorization"), "Bearer "))
	return true
}

func (c *clientCredentials) String() string {
	return "a client credentials token from " + c.spec.TokenURL
}

// failing is a credential that cannot be produced, for a spec that does not
// hold together.
type failing struct{ err error }

func (f failing) Authorize(context.Context, http.Header) error { return f.err }
func (failing) Refused(http.Header) bool                       { return false }
func (failing) String() string                                 { return "an invalid spec.auth" }

// noSecrets is what a process started without a namespace to read Secrets from
// has.
type noSecrets struct{}

func (noSecrets) Value(context.Context, gatev1alpha1.SecretKeyRef) (string, error) {
	return "", errors.New("this process has no namespace to read Secrets from; start it with --secret-namespace")
}

func (noSecrets) Invalidate(gatev1alpha1.SecretKeyRef) bool { return false }

type callerKey struct{}

// WithCaller records the Authorization header of the request a tool call serves,
// for a Passthrough backend to forward.
func WithCaller(ctx context.Context, authorization string) context.Context {
	return context.WithValue(ctx, callerKey{}, authorization)
}

// Caller returns the Authorization header WithCaller recorded.
func Caller(ctx context.Context) (string, bool) {
	authorization, ok := ctx.Value(callerKey{}).(string)
	return authorization, ok && authorization != ""
}
