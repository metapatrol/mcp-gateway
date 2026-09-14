package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MCPToolSetSpec selects tools from exactly one MCPServer.
//
// One server, never several: a tool set is a technical toolbox for one backend.
// Putting tools of different backends together is what MCPCapability is for,
// and keeping the two apart means a tool set can be verified against a single
// catalog.
type MCPToolSetSpec struct {
	// ServerRef is the name of the MCPServer the tools come from.
	// +kubebuilder:validation:MinLength=1
	ServerRef string `json:"serverRef"`

	// Tools are the names of the tools selected, exactly as the backend lists
	// them. A name the backend does not expose makes the tool set Ready=False,
	// and the gateway exposes the tools that do exist.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	// +listType=set
	Tools []string `json:"tools"`
}

// MCPToolSetStatus is the result of verifying the selection against the
// server's current catalog.
type MCPToolSetStatus struct {
	// ObservedGeneration is the generation this status describes.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// CatalogRevision is the server catalog the tools were last verified
	// against.
	// +optional
	CatalogRevision string `json:"catalogRevision,omitempty"`

	// MissingTools are the selected tools the server does not currently expose.
	// +optional
	MissingTools []string `json:"missingTools,omitempty"`

	// Conditions: Valid and Ready.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=mcpts,categories=mcp-gate
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Server",type=string,JSONPath=`.spec.serverRef`
// +kubebuilder:printcolumn:name="Valid",type=string,JSONPath=`.status.conditions[?(@.type=="Valid")].status`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MCPToolSet is a curated subset of one MCPServer's tools.
type MCPToolSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPToolSetSpec   `json:"spec,omitempty"`
	Status MCPToolSetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MCPToolSetList contains a list of MCPToolSet.
type MCPToolSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPToolSet `json:"items"`
}
