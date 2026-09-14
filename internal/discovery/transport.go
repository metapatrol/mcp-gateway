package discovery

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"forgejo.metapatrol.local/platform/mcp-gate/internal/credentials"
)

// RefusedError is a request the backend answered with 401 or 403.
type RefusedError struct {
	StatusCode int
	// Credential says what the request carried; empty when it carried nothing.
	Credential string
}

func (e *RefusedError) Error() string {
	status := fmt.Sprintf("%d %s", e.StatusCode, http.StatusText(e.StatusCode))
	if e.Credential == "" {
		return "the backend refused a request without credentials (" + status + "): set spec.auth"
	}
	return fmt.Sprintf("the backend refused %s (%s)", e.Credential, status)
}

// CredentialError is a credential that could not be produced: a Secret that is
// missing, a token endpoint that refused the client.
type CredentialError struct{ Err error }

func (e *CredentialError) Error() string { return "spec.auth: " + e.Err.Error() }
func (e *CredentialError) Unwrap() error { return e.Err }

// authFailure reports whether err is a refusal or a credential failure. Neither
// is cured by a new session.
func authFailure(err error) bool {
	var refused *RefusedError
	var credential *CredentialError
	return errors.As(err, &refused) || errors.As(err, &credential)
}

// authTransport authenticates the requests of one session.
//
// It also turns a 401 or 403 into an error of its own instead of a response.
// The SDK takes a non-2xx answer it cannot decode as the end of the connection,
// and would tear down a session shared by every caller over one refused request;
// an error from the transport it takes as a request that did not go through,
// which leaves the session as it was.
type authTransport struct {
	base http.RoundTripper
	// host is the backend's scheme and host. Credentials go nowhere else, so
	// a redirect to another host does not take them along.
	host       url.URL
	credential credentials.Credential
}

func newAuthTransport(base http.RoundTripper, endpoint string, credential credentials.Credential) *authTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	t := &authTransport{base: base, credential: credential}
	if u, err := url.Parse(endpoint); err == nil {
		t.host = url.URL{Scheme: u.Scheme, Host: u.Host}
	}
	return t
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		out := req.Clone(req.Context())
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			out.Body = body
		}
		sendsCredential := t.credential != nil && req.URL.Scheme == t.host.Scheme && req.URL.Host == t.host.Host
		if sendsCredential {
			if err := t.credential.Authorize(req.Context(), out.Header); err != nil {
				closeBody(req)
				return nil, &CredentialError{Err: err}
			}
		}

		resp, err := t.base.RoundTrip(out)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			return resp, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()

		replayable := req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 && sendsCredential && replayable && t.credential.Refused(out.Header) {
			continue
		}
		refused := &RefusedError{StatusCode: resp.StatusCode}
		if sendsCredential {
			refused.Credential = t.credential.String()
		}
		return nil, refused
	}
}

func closeBody(req *http.Request) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
}
