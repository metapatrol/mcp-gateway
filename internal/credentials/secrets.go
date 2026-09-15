package credentials

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
)

// Secrets reads credential values out of Secrets.
type Secrets interface {
	// Value returns the value under ref's key, with surrounding whitespace
	// removed: a key written from a file usually ends in a newline no
	// provider expects.
	Value(ctx context.Context, ref mcpv1alpha1.SecretKeyRef) (string, error)

	// Invalidate tells the reader a value it returned was refused, so the next
	// Value reads the Secret again. It reports whether it will: a Secret read
	// only moments ago is not read again, since what came back then is what
	// would come back now.
	Invalidate(ref mcpv1alpha1.SecretKeyRef) bool
}

// SecretReader reads Secrets of one namespace through the API server and keeps
// them for a while.
//
// It reads with get, one Secret at a time, rather than watching: the gateway
// needs a handful of Secrets, and an informer would hold every Secret of the
// namespace in memory — Helm release secrets included — and need list and
// watch on all of them.
type SecretReader struct {
	reader    client.Reader
	namespace string
	ttl       time.Duration
	now       func() time.Time

	mu      sync.Mutex
	secrets map[string]cachedSecret
}

type cachedSecret struct {
	data map[string][]byte
	// missing records a Secret that does not exist, so that a backend
	// configured with a typo does not cost an API request per tool call.
	missing bool
	read    time.Time
}

const (
	// DefaultSecretTTL is how long a Secret is used before it is read again.
	// A rotated key is picked up within it, or at once when the backend
	// refuses the old one.
	DefaultSecretTTL = time.Minute
	// missingSecretTTL is how long a Secret found missing stays missing.
	missingSecretTTL = 10 * time.Second
	// minRereadAge is how old a read must be before a refusal triggers another.
	minRereadAge = 5 * time.Second
)

// NewSecretReader creates a reader for Secrets in namespace. Pass the manager's
// API reader, not its cached client, which would start the informer this type
// exists to avoid.
func NewSecretReader(reader client.Reader, namespace string) *SecretReader {
	return &SecretReader{
		reader:    reader,
		namespace: namespace,
		ttl:       DefaultSecretTTL,
		now:       time.Now,
		secrets:   map[string]cachedSecret{},
	}
}

// Namespace is where the Secrets are read from.
func (r *SecretReader) Namespace() string { return r.namespace }

// Value implements Secrets.
func (r *SecretReader) Value(ctx context.Context, ref mcpv1alpha1.SecretKeyRef) (string, error) {
	secret, err := r.secret(ctx, ref.Name)
	if err != nil {
		return "", err
	}
	if secret.missing {
		return "", fmt.Errorf("secret %q not found in namespace %q", ref.Name, r.namespace)
	}
	raw, ok := secret.data[ref.Key]
	if !ok {
		return "", fmt.Errorf("secret %q in namespace %q has no key %q%s", ref.Name, r.namespace, ref.Key, keysHint(secret.data))
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", fmt.Errorf("key %q of secret %q in namespace %q is empty", ref.Key, ref.Name, r.namespace)
	}
	return value, nil
}

// keysHint lists the keys a Secret does have. A reference that names the wrong
// key is the likeliest mistake with a Secret some other tool wrote, and the
// names alone, never the values, are enough to fix it.
func keysHint(data map[string][]byte) string {
	if len(data) == 0 {
		return " (it has no keys)"
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return fmt.Sprintf(" (it has %s)", strings.Join(keys, ", "))
}

func (r *SecretReader) secret(ctx context.Context, name string) (cachedSecret, error) {
	r.mu.Lock()
	cached, ok := r.secrets[name]
	r.mu.Unlock()
	if ok {
		ttl := r.ttl
		if cached.missing {
			ttl = missingSecretTTL
		}
		if r.now().Sub(cached.read) < ttl {
			return cached, nil
		}
	}

	var secret corev1.Secret
	err := r.reader.Get(ctx, types.NamespacedName{Namespace: r.namespace, Name: name}, &secret)
	switch {
	case apierrors.IsNotFound(err):
		cached = cachedSecret{missing: true, read: r.now()}
	case err != nil:
		// Not kept: a timeout or a missing permission says nothing about
		// the Secret, and the next attempt may well succeed.
		return cachedSecret{}, fmt.Errorf("reading secret %q in namespace %q: %w", name, r.namespace, err)
	default:
		cached = cachedSecret{data: secret.Data, read: r.now()}
	}

	r.mu.Lock()
	r.secrets[name] = cached
	r.mu.Unlock()
	return cached, nil
}

// Invalidate implements Secrets.
func (r *SecretReader) Invalidate(ref mcpv1alpha1.SecretKeyRef) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cached, ok := r.secrets[ref.Name]
	if !ok {
		return true
	}
	if r.now().Sub(cached.read) < minRereadAge {
		return false
	}
	delete(r.secrets, ref.Name)
	return true
}

// serviceAccountNamespaceFile is where Kubernetes tells a pod its namespace.
const serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// PodNamespace returns the namespace this process runs in: POD_NAMESPACE when
// set, the service account's namespace file otherwise, and "" outside a pod.
func PodNamespace() string {
	if ns := strings.TrimSpace(os.Getenv("POD_NAMESPACE")); ns != "" {
		return ns
	}
	if b, err := os.ReadFile(serviceAccountNamespaceFile); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}
