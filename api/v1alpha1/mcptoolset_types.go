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

	// Tools selects tools by name or by glob pattern: "*" matches any run of
	// characters, dots included, and "?" exactly one — so "code.*" selects every
	// tool whose name starts with "code.", and "*" selects them all.
	//
	// An exact name the backend does not expose makes the tool set Ready=False,
	// as does a pattern that selects nothing. The gateway exposes what does
	// resolve. A pattern also selects tools the backend adds later, without a
	// change here; status.resolvedTools shows what it selects right now.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	// +listType=set
	Tools []string `json:"tools"`

	// Exclude removes tools from the selection, by name or glob pattern, and
	// wins over tools: "artifact_*" with exclude "artifact_delete" selects every
	// artifact tool but that one — including artifact tools added later.
	// +optional
	// +kubebuilder:validation:items:MinLength=1
	// +listType=set
	Exclude []string `json:"exclude,omitempty"`
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

	// ResolvedTools are the tools the tool set selects from the catalog it was
	// last verified against, sorted: the names and pattern matches the server
	// exposes, minus the exclusions. It is what a grant on this tool set hands
	// out.
	// +optional
	ResolvedTools []string `json:"resolvedTools,omitempty"`

	// ResolvedToolCount is the length of resolvedTools, for kubectl's columns.
	// +optional
	ResolvedToolCount int `json:"resolvedToolCount"`

	// MissingTools are the exact names in spec.tools the server does not
	// currently expose.
	// +optional
	MissingTools []string `json:"missingTools,omitempty"`

	// UnmatchedPatterns are the patterns in spec.tools that select no tool of
	// the current catalog — none matches, or every match is excluded.
	// +optional
	UnmatchedPatterns []string `json:"unmatchedPatterns,omitempty"`

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
// +kubebuilder:printcolumn:name="Tools",type=integer,JSONPath=`.status.resolvedToolCount`
// +kubebuilder:printcolumn:name="Valid",type=string,JSONPath=`.status.conditions[?(@.type=="Valid")].status`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,priority=1
// +kubebuilder:printcolumn:name="Resolved",type=string,JSONPath=`.status.resolvedTools`,priority=1
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
