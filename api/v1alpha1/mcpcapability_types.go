package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MCPCapabilitySpec combines tool sets into something an agent can do.
//
// A capability names an ability — code-research, artifact-authoring — rather
// than a backend, and may draw on as many backends as the ability needs.
type MCPCapabilitySpec struct {
	// Description says what the capability is for. It is shown by kubectl and
	// never sent to agents.
	// +optional
	// +kubebuilder:validation:MaxLength=512
	Description string `json:"description,omitempty"`

	// ToolSets are the names of the MCPToolSets this capability consists of.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	// +listType=set
	ToolSets []string `json:"toolSets"`
}

// MCPCapabilityStatus reports whether the tool sets resolve and are available.
type MCPCapabilityStatus struct {
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
// +kubebuilder:resource:scope=Cluster,shortName=mcpcap,categories=mcp-gateway
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="ToolSets",type=string,JSONPath=`.spec.toolSets`
// +kubebuilder:printcolumn:name="Valid",type=string,JSONPath=`.status.conditions[?(@.type=="Valid")].status`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MCPCapability is a reusable ability built from one or more MCPToolSets.
type MCPCapability struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPCapabilitySpec   `json:"spec,omitempty"`
	Status MCPCapabilityStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MCPCapabilityList contains a list of MCPCapability.
type MCPCapabilityList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPCapability `json:"items"`
}
