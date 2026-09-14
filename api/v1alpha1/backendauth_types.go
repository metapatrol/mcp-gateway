package v1alpha1

// BackendAuthType names how requests to a backend are authenticated.
// +kubebuilder:validation:Enum=None;Passthrough;ClientCredentials;APIKey
type BackendAuthType string

const (
	// BackendAuthNone sends requests without credentials.
	BackendAuthNone BackendAuthType = "None"
	// BackendAuthPassthrough forwards the calling agent's bearer token.
	BackendAuthPassthrough BackendAuthType = "Passthrough"
	// BackendAuthClientCredentials authenticates as an OAuth client of its own.
	BackendAuthClientCredentials BackendAuthType = "ClientCredentials"
	// BackendAuthAPIKey sends a static key from a Secret.
	BackendAuthAPIKey BackendAuthType = "APIKey"
)

// BackendAuth is a discriminated union: type says which of the other fields
// applies, and only that one may be set.
//
// Each kind has a field of its own rather than all of them sharing one generic
// block, so the API server validates what is written against the kind it is
// written for and `kubectl explain` can describe it.
//
// +kubebuilder:validation:XValidation:rule="self.type == 'Passthrough' || !has(self.passthrough)",message="passthrough may only be set with type Passthrough"
// +kubebuilder:validation:XValidation:rule="self.type == 'ClientCredentials' ? has(self.clientCredentials) : !has(self.clientCredentials)",message="clientCredentials is required with type ClientCredentials, and may only be set with it"
// +kubebuilder:validation:XValidation:rule="self.type == 'APIKey' ? has(self.apiKey) : !has(self.apiKey)",message="apiKey is required with type APIKey, and may only be set with it"
type BackendAuth struct {
	// Type is how requests to the backend are authenticated:
	//
	//   None               no credentials
	//   Passthrough        the calling agent's own bearer token, on tool calls
	//   ClientCredentials  a token the gateway obtains as an OAuth client of its own
	//   APIKey             a static key from a Secret
	// +kubebuilder:default=None
	Type BackendAuthType `json:"type"`

	// Passthrough configures type Passthrough.
	// +optional
	Passthrough *PassthroughAuth `json:"passthrough,omitempty"`

	// ClientCredentials configures type ClientCredentials.
	// +optional
	ClientCredentials *ClientCredentialsAuth `json:"clientCredentials,omitempty"`

	// APIKey configures type APIKey.
	// +optional
	APIKey *APIKeyAuth `json:"apiKey,omitempty"`
}

// PassthroughAuth forwards each agent's own token to the backend.
//
// The token goes out exactly as the agent presented it, on the requests made
// for that agent's tool calls. The gateway does not inspect it on the backend's
// behalf: which tokens the backend accepts is the backend's to decide. Each
// agent gets a session of its own with the backend, since a session is bound to
// the identity that opened it.
type PassthroughAuth struct {
	// Discovery authenticates the requests that read the catalog. Those are
	// made in the background, by the controller and the gateway, with no agent
	// and therefore no token to pass through. Left out, they carry no
	// credentials, which suits a backend that lists its tools openly and
	// guards only the calls.
	//
	// A backend whose tool list depends on the caller is seen through this
	// credential alone: agents are offered what it can see.
	// +optional
	Discovery *DiscoveryAuth `json:"discovery,omitempty"`
}

// DiscoveryAuth is the credential catalog reads use under Passthrough: exactly
// one of clientCredentials or apiKey.
//
// +kubebuilder:validation:XValidation:rule="has(self.clientCredentials) != has(self.apiKey)",message="set exactly one of clientCredentials or apiKey"
type DiscoveryAuth struct {
	// ClientCredentials authenticates catalog reads as an OAuth client.
	// +optional
	ClientCredentials *ClientCredentialsAuth `json:"clientCredentials,omitempty"`

	// APIKey authenticates catalog reads with a static key.
	// +optional
	APIKey *APIKeyAuth `json:"apiKey,omitempty"`
}

// ClientCredentialsAuth obtains tokens with the OAuth 2.0 client credentials
// grant (RFC 6749 §4.4) and sends them as bearer tokens.
//
// +kubebuilder:validation:XValidation:rule="has(self.clientID) != has(self.clientIDRef)",message="set exactly one of clientID or clientIDRef"
type ClientCredentialsAuth struct {
	// TokenURL is the provider's token endpoint.
	// +kubebuilder:validation:Pattern=`^https?://[^\s]+$`
	TokenURL string `json:"tokenURL"`

	// ClientID is the OAuth client ID, written out.
	// +optional
	// +kubebuilder:validation:MinLength=1
	ClientID string `json:"clientID,omitempty"`

	// ClientIDRef reads the client ID from a Secret instead — for providers,
	// or operators like hydra-maester, that generate the ID and store it beside
	// the secret.
	// +optional
	ClientIDRef *SecretKeyRef `json:"clientIDRef,omitempty"`

	// ClientSecretRef is where the client secret is.
	ClientSecretRef SecretKeyRef `json:"clientSecretRef"`

	// Scopes are requested with each token.
	// +optional
	// +listType=set
	Scopes []string `json:"scopes,omitempty"`

	// Audience is requested with each token, as the audience parameter
	// providers such as Ory Hydra accept. Several are sent space-separated.
	// +optional
	// +listType=set
	Audience []string `json:"audience,omitempty"`
}

// APIKeyAuth sends a static key in a header.
type APIKeyAuth struct {
	// SecretRef is where the key is.
	SecretRef SecretKeyRef `json:"secretRef"`

	// Header is the request header the key is sent in.
	// +optional
	// +kubebuilder:default=Authorization
	// +kubebuilder:validation:MinLength=1
	Header string `json:"header,omitempty"`

	// Prefix goes in front of the key. Left out, it is "Bearer " when the
	// header is Authorization and nothing otherwise; set it to "" to send a key
	// in Authorization bare.
	// +optional
	Prefix *string `json:"prefix,omitempty"`
}

// SecretKeyRef names one key of one Secret.
//
// There is no namespace. Secrets are read from the namespace the controller and
// the gateway run in: an MCPServer is cluster-scoped, and a reference that could
// name any namespace would let whoever may create one have the gateway read any
// Secret in the cluster.
type SecretKeyRef struct {
	// Name of the Secret.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key is the data key holding the value. Named explicitly, so a Secret
	// written by some other tool — CLIENT_SECRET, client-secret, token — is
	// used as it is.
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// AuthType returns the type in effect: None when auth is left out.
func (s *MCPServer) AuthType() BackendAuthType {
	if s.Spec.Auth == nil || s.Spec.Auth.Type == "" {
		return BackendAuthNone
	}
	return s.Spec.Auth.Type
}
