package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MCPGrantSpec assigns capabilities to an authenticated subject.
type MCPGrantSpec struct {
	// Subject is the identity the gateway derives from a caller's access
	// token — by default the token's sub claim, which for the client
	// credentials grant is the OAuth client ID.
	//
	// It is an opaque string on purpose. Nothing here says which OAuth
	// provider issued the token or which claim it came from; that mapping is
	// the gateway's configuration, and a grant written today keeps meaning the
	// same thing when the provider changes.
	// +kubebuilder:validation:MinLength=1
	Subject string `json:"subject"`

	// Capabilities are the names of the MCPCapabilities granted.
	//
	// A name that does not resolve grants nothing, and the rest of the grant
	// still applies: an unresolved reference can narrow what a subject gets,
	// never widen it.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	// +listType=set
	Capabilities []string `json:"capabilities"`
}

// MCPGrantStatus reports whether the grant resolves and is available.
type MCPGrantStatus struct {
	// ObservedGeneration is the generation this status describes.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions: Valid and Ready.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=mcpg,categories=mcp-gate
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Subject",type=string,JSONPath=`.spec.subject`
// +kubebuilder:printcolumn:name="Capabilities",type=string,JSONPath=`.spec.capabilities`
// +kubebuilder:printcolumn:name="Valid",type=string,JSONPath=`.status.conditions[?(@.type=="Valid")].status`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MCPGrant binds capabilities to a subject. Several grants may name the same
// subject; the subject gets the union of them.
type MCPGrant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPGrantSpec   `json:"spec,omitempty"`
	Status MCPGrantStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MCPGrantList contains a list of MCPGrant.
type MCPGrantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPGrant `json:"items"`
}
