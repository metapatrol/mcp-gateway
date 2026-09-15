package discovery

import "net/http"

// MaxIdleConnsPerBackend is how many idle connections to one backend are kept
// for the next request.
//
// Every tool call is a request of its own, so as many connections are open to a
// backend as it has calls in flight. The standard library keeps two of them
// once those calls are done; the rest are closed, and the next burst of calls
// opens them again — a TCP handshake each, a TLS handshake on top towards an
// https backend, and a socket in TIME_WAIT for every one closed.
//
// The figure is generous on purpose. An idle connection costs a few kilobytes
// and is closed after the transport's idle timeout of 90 seconds, so a large
// pool only holds memory for a short while after a peak, where a small one
// churns through connections during every peak.
const MaxIdleConnsPerBackend = 1000

// NewHTTPClient returns the client the controller and the gateway reach backends
// with: the standard transport, keeping up to MaxIdleConnsPerBackend idle
// connections to each backend, however many backends there are.
//
// It sets no Timeout. Notification streams stay open for as long as they are
// needed, and deadlines come from contexts.
func NewHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = MaxIdleConnsPerBackend
	// The standard transport also caps idle connections across all hosts at
	// 100, which would undo the per-backend figure.
	transport.MaxIdleConns = 0
	return &http.Client{Transport: transport}
}
