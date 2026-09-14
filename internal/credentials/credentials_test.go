package credentials

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
)

// countingReader counts the Gets that reach the API.
type countingReader struct {
	client.Client
	gets atomic.Int64
}

func (c *countingReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.gets.Add(1)
	return c.Client.Get(ctx, key, obj, opts...)
}

func newReader(t *testing.T, objs ...client.Object) (*SecretReader, *countingReader, *time.Time) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	c := &countingReader{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()}
	r := NewSecretReader(c, "mcp-gate")
	now := time.Now()
	r.now = func() time.Time { return now }
	return r, c, &now
}

func secret(name string, data map[string]string) *corev1.Secret {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "mcp-gate"}, Data: map[string][]byte{}}
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s
}

func ref(name, key string) gatev1alpha1.SecretKeyRef {
	return gatev1alpha1.SecretKeyRef{Name: name, Key: key}
}

// A Secret some other tool wrote is used as it is: the reference names the key.
func TestSecretReader_ReadsTheNamedKey(t *testing.T) {
	r, _, _ := newReader(t, secret("memory-client", map[string]string{
		"CLIENT_ID":     "memory-gateway",
		"CLIENT_SECRET": "s3cret\n",
	}))

	id, err := r.Value(context.Background(), ref("memory-client", "CLIENT_ID"))
	require.NoError(t, err)
	assert.Equal(t, "memory-gateway", id)
	value, err := r.Value(context.Background(), ref("memory-client", "CLIENT_SECRET"))
	require.NoError(t, err)
	assert.Equal(t, "s3cret", value, "a trailing newline from a file is not part of the secret")
}

func TestSecretReader_AWrongKeyListsTheKeysThereAre(t *testing.T) {
	r, _, _ := newReader(t, secret("memory-client", map[string]string{"CLIENT_ID": "id", "CLIENT_SECRET": "s3cret"}))

	_, err := r.Value(context.Background(), ref("memory-client", "client-secret"))
	require.Error(t, err)
	assert.Equal(t, `secret "memory-client" in namespace "mcp-gate" has no key "client-secret" (it has CLIENT_ID, CLIENT_SECRET)`, err.Error())
	assert.NotContains(t, err.Error(), "s3cret")
}

func TestSecretReader_AnEmptyValueIsAnError(t *testing.T) {
	r, _, _ := newReader(t, secret("memory-client", map[string]string{"token": " \n"}))
	_, err := r.Value(context.Background(), ref("memory-client", "token"))
	assert.EqualError(t, err, `key "token" of secret "memory-client" in namespace "mcp-gate" is empty`)
}

func TestSecretReader_KeepsSecretsForTheTTL(t *testing.T) {
	r, c, now := newReader(t, secret("memory-client", map[string]string{"CLIENT_ID": "id", "CLIENT_SECRET": "s3cret"}))
	ctx := context.Background()

	for range 3 {
		_, err := r.Value(ctx, ref("memory-client", "CLIENT_ID"))
		require.NoError(t, err)
		_, err = r.Value(ctx, ref("memory-client", "CLIENT_SECRET"))
		require.NoError(t, err)
	}
	assert.Equal(t, int64(1), c.gets.Load(), "both keys of one Secret come from one read")

	rotated := secret("memory-client", map[string]string{"CLIENT_ID": "id", "CLIENT_SECRET": "rotated"})
	require.NoError(t, c.Update(ctx, rotated))
	*now = now.Add(DefaultSecretTTL)
	value, err := r.Value(ctx, ref("memory-client", "CLIENT_SECRET"))
	require.NoError(t, err)
	assert.Equal(t, "rotated", value)
	assert.Equal(t, int64(2), c.gets.Load())
}

func TestSecretReader_AMissingSecretIsRememberedBriefly(t *testing.T) {
	r, c, now := newReader(t)
	ctx := context.Background()

	for range 3 {
		_, err := r.Value(ctx, ref("memory-client", "token"))
		assert.EqualError(t, err, `secret "memory-client" not found in namespace "mcp-gate"`)
	}
	assert.Equal(t, int64(1), c.gets.Load())

	require.NoError(t, c.Create(ctx, secret("memory-client", map[string]string{"token": "t"})))
	*now = now.Add(missingSecretTTL)
	value, err := r.Value(ctx, ref("memory-client", "token"))
	require.NoError(t, err)
	assert.Equal(t, "t", value, "a Secret created later is found within seconds, not a whole TTL")
}

func TestSecretReader_InvalidateRereadsUnlessJustRead(t *testing.T) {
	r, c, now := newReader(t, secret("memory-client", map[string]string{"token": "t"}))
	ctx := context.Background()
	_, err := r.Value(ctx, ref("memory-client", "token"))
	require.NoError(t, err)

	assert.False(t, r.Invalidate(ref("memory-client", "token")), "what was just read would be read again")
	*now = now.Add(minRereadAge)
	assert.True(t, r.Invalidate(ref("memory-client", "token")))
	_, err = r.Value(ctx, ref("memory-client", "token"))
	require.NoError(t, err)
	assert.Equal(t, int64(2), c.gets.Load())
}

func authorize(t *testing.T, c Credential) http.Header {
	t.Helper()
	header := http.Header{}
	require.NoError(t, c.Authorize(context.Background(), header))
	return header
}

func TestAPIKey_HeaderAndPrefix(t *testing.T) {
	r, _, _ := newReader(t, secret("memory", map[string]string{"key": "k-123"}))
	build := func(header string, prefix *string) Credential {
		return Build(&gatev1alpha1.BackendAuth{
			Type:   gatev1alpha1.BackendAuthAPIKey,
			APIKey: &gatev1alpha1.APIKeyAuth{SecretRef: ref("memory", "key"), Header: header, Prefix: prefix},
		}, r, nil).Credential
	}

	assert.Equal(t, "Bearer k-123", authorize(t, build("", nil)).Get("Authorization"))
	assert.Equal(t, "k-123", authorize(t, build("Authorization", ptr.To(""))).Get("Authorization"))
	assert.Equal(t, "Token k-123", authorize(t, build("Authorization", ptr.To("Token "))).Get("Authorization"))
	custom := authorize(t, build("X-API-Key", nil))
	assert.Equal(t, "k-123", custom.Get("X-API-Key"))
	assert.Empty(t, custom.Get("Authorization"))
}

func TestClientCredentials_TheClientIDMayBeWrittenOut(t *testing.T) {
	var issued atomic.Int64
	tokenURL := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, secret, _ := r.BasicAuth()
		if id != "memory-gateway" || secret != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		issued.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "token_type": "bearer", "expires_in": 3600})
	}))
	t.Cleanup(tokenURL.Close)
	r, _, _ := newReader(t, secret("memory-client", map[string]string{"secret": "s3cret"}))

	c := Build(&gatev1alpha1.BackendAuth{
		Type: gatev1alpha1.BackendAuthClientCredentials,
		ClientCredentials: &gatev1alpha1.ClientCredentialsAuth{
			TokenURL:        tokenURL.URL,
			ClientID:        "memory-gateway",
			ClientSecretRef: ref("memory-client", "secret"),
		},
	}, r, nil).Credential

	header := authorize(t, c)
	assert.Equal(t, "Bearer tok", header.Get("Authorization"))
	authorize(t, c)
	assert.Equal(t, int64(1), issued.Load())
	assert.True(t, c.Refused(header))
	authorize(t, c)
	assert.Equal(t, int64(2), issued.Load(), "a refused token is replaced")
	assert.NotContains(t, c.String(), "s3cret")
}

func TestClientCredentials_ARefusedClientIsAnError(t *testing.T) {
	tokenURL := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	t.Cleanup(tokenURL.Close)
	r, _, _ := newReader(t, secret("memory-client", map[string]string{"id": "x", "secret": "y"}))

	c := Build(&gatev1alpha1.BackendAuth{
		Type: gatev1alpha1.BackendAuthClientCredentials,
		ClientCredentials: &gatev1alpha1.ClientCredentialsAuth{
			TokenURL:        tokenURL.URL,
			ClientIDRef:     ptr.To(ref("memory-client", "id")),
			ClientSecretRef: ref("memory-client", "secret"),
		},
	}, r, nil).Credential

	err := c.Authorize(context.Background(), http.Header{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "obtaining a token from "+tokenURL.URL)
	assert.Contains(t, err.Error(), "invalid_client")
}

func TestBuild(t *testing.T) {
	none := Build(nil, nil, nil)
	assert.Equal(t, gatev1alpha1.BackendAuthNone, none.Type)
	assert.Nil(t, none.Credential)
	assert.False(t, none.Passthrough())
	assert.Equal(t, none.Fingerprint, Build(&gatev1alpha1.BackendAuth{Type: gatev1alpha1.BackendAuthNone}, nil, nil).Fingerprint,
		"auth left out and type None are one configuration")

	passthrough := Build(&gatev1alpha1.BackendAuth{Type: gatev1alpha1.BackendAuthPassthrough}, nil, nil)
	assert.True(t, passthrough.Passthrough())
	assert.Nil(t, passthrough.Credential, "without a discovery credential, catalog reads carry none")

	apiKey := &gatev1alpha1.BackendAuth{
		Type:   gatev1alpha1.BackendAuthAPIKey,
		APIKey: &gatev1alpha1.APIKeyAuth{SecretRef: ref("memory", "key")},
	}
	err := Build(apiKey, nil, nil).Credential.Authorize(context.Background(), http.Header{})
	assert.ErrorContains(t, err, "--secret-namespace", "a process without Secrets says how to give it some")

	other := apiKey.DeepCopy()
	other.APIKey.SecretRef.Key = "other"
	assert.Equal(t, Build(apiKey, nil, nil).Fingerprint, Build(apiKey.DeepCopy(), nil, nil).Fingerprint)
	assert.NotEqual(t, Build(apiKey, nil, nil).Fingerprint, Build(other, nil, nil).Fingerprint)

	broken := Build(&gatev1alpha1.BackendAuth{Type: gatev1alpha1.BackendAuthClientCredentials}, nil, nil)
	assert.EqualError(t, broken.Credential.Authorize(context.Background(), http.Header{}), "clientCredentials is required with type ClientCredentials")
}

func TestCaller(t *testing.T) {
	_, ok := Caller(context.Background())
	assert.False(t, ok)
	_, ok = Caller(WithCaller(context.Background(), ""))
	assert.False(t, ok)
	authorization, ok := Caller(WithCaller(context.Background(), "Bearer abc"))
	assert.True(t, ok)
	assert.Equal(t, "Bearer abc", authorization)

	header := authorize(t, Forward("Bearer abc"))
	assert.Equal(t, "Bearer abc", header.Get("Authorization"))
	assert.False(t, Forward("Bearer abc").Refused(header), "there is no other caller token to try")
}
