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

	// Prefix goes in front of the name of every tool the set selects, as agents
	// see it: with prefix "engram_", the backend's memory_search is offered as
	// engram_memory_search. Tools, exclude and aliases keep using the backend's
	// names. It is how two backends that name a tool alike can both be granted
	// to one subject, where otherwise neither tool would be exposed.
	// +optional
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_.-]{0,64}$`
	Prefix string `json:"prefix,omitempty"`

	// Aliases offers single tools under a name of their own: the backend's name
	// of a tool the set selects, mapped to the name agents see. An alias wins
	// over prefix. A key names one tool, never a pattern; a key the set does not
	// select makes the tool set Ready=False, and so do two tools the set would
	// expose under one name.
	// +optional
	// +kubebuilder:validation:MaxProperties=128
	// +kubebuilder:validation:XValidation:rule="self.all(k, !k.contains('*') && !k.contains('?'))",message="aliases name exact tools, not patterns"
	Aliases map[string]ToolName `json:"aliases,omitempty"`
}

// ToolName is a tool's name as agents see it.
// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_.-]{1,128}$`
type ToolName string

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
	// exposes, minus the exclusions, under the names agents see — after prefix
	// and aliases. It is what a grant on this tool set hands out.
	// +optional
	ResolvedTools []string `json:"resolvedTools,omitempty"`

	// BackendNames maps each resolved tool offered under a name other than the
	// backend's — through prefix or an alias — to the backend's name.
	// +optional
	BackendNames map[string]string `json:"backendNames,omitempty"`

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

	// UnusedAliases are the keys of spec.aliases that name no tool the set
	// selects from the current catalog.
	// +optional
	UnusedAliases []string `json:"unusedAliases,omitempty"`

	// ConflictingNames are the names under which the set itself would expose
	// more than one tool — two aliases alike, or an alias equal to another
	// tool's prefixed name. None of those tools is exposed.
	// +optional
	ConflictingNames []string `json:"conflictingNames,omitempty"`

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
