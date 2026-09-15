package v1alpha1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MCPServerSpec says where an MCP backend is and how to reach it.
//
// It deliberately carries no list of tools. The backend is the one source of
// truth for what it offers; the controller and the gateway both read the
// catalog from it, and a copy here would be a second source that drifts.
type MCPServerSpec struct {
	// Endpoint is the URL of the backend's Streamable HTTP MCP endpoint, for
	// example http://source-intelligence.mcp-system.svc/mcp.
	//
	// The gateway is the only thing meant to reach it: agents talk to the
	// gateway, and a NetworkPolicy on the backend's side should say so.
	// +kubebuilder:validation:Pattern=`^https?://[^\s]+$`
	Endpoint string `json:"endpoint"`

	// DiscoveryInterval is how often the catalog is re-read when the backend
	// does not announce changes itself. A backend that sends
	// notifications/tools/list_changed is re-read as soon as it does.
	// +optional
	// +kubebuilder:default="1m"
	DiscoveryInterval *metav1.Duration `json:"discoveryInterval,omitempty"`

	// Auth says how the gateway and the controller authenticate to the
	// backend. Left out, they do not: type None.
	// +optional
	Auth *BackendAuth `json:"auth,omitempty"`
}

// MCPServerStatus is what the controller last learned from the backend.
//
// It is compact on purpose. Tool names, descriptions and schemas stay in memory
// in the processes that need them; the Kubernetes API is the desired-state API
// and not a cache for catalogs that can run to megabytes.
type MCPServerStatus struct {
	// ObservedGeneration is the generation this status describes.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Available reports whether the most recent discovery succeeded.
	//
	// The default is what makes a server that was never reachable show false
	// rather than nothing: status is written as a patch against the previous
	// status, and a false that never changed is never in one.
	// +optional
	// +kubebuilder:default=false
	Available bool `json:"available"`

	// DiscoveredTools is how many tools the last successful discovery found.
	// +optional
	DiscoveredTools int `json:"discoveredTools,omitempty"`

	// CatalogRevision identifies the catalog the last successful discovery
	// found: a sha256 over the tool definitions and the server's instructions,
	// so any change to a name, a description, a schema or the instructions
	// produces a new one. The gateway re-reads a backend whose revision here
	// differs from the one it holds.
	// +optional
	CatalogRevision string `json:"catalogRevision,omitempty"`

	// LastDiscoveryTime is when discovery last succeeded. DiscoveredTools and
	// CatalogRevision describe that moment, not the latest attempt — while
	// the backend is down they keep saying what it offered when it was up.
	// +optional
	LastDiscoveryTime *metav1.Time `json:"lastDiscoveryTime,omitempty"`

	// Conditions: Ready.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=mcpsrv,categories=mcp-gateway
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.spec.endpoint`
// +kubebuilder:printcolumn:name="Available",type=boolean,JSONPath=`.status.available`
// +kubebuilder:printcolumn:name="Tools",type=integer,JSONPath=`.status.discoveredTools`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MCPServer registers an MCP backend with the gateway.
//
// It is cluster-scoped, like the other three kinds. The gateway can reach every
// backend it routes to, so whoever may create an MCPServer decides what that
// network position is used for; a namespaced kind would hand that decision to
// every namespace. See docs/architecture.md.
type MCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPServerSpec   `json:"spec,omitempty"`
	Status MCPServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MCPServerList contains a list of MCPServer.
type MCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPServer `json:"items"`
}

// DefaultDiscoveryInterval applies when spec.discoveryInterval is unset — which
// the schema default prevents for anything the API server stores, but not for
// an object built in a test.
const DefaultDiscoveryInterval = time.Minute

// MinDiscoveryInterval is the floor under spec.discoveryInterval. The schema
// cannot compare durations, so a manifest asking for 100ms is accepted and then
// clamped here, rather than turning the controller and every gateway replica
// into a load generator against the backend.
const MinDiscoveryInterval = 10 * time.Second

// Interval returns spec.discoveryInterval, defaulted and clamped.
func (s *MCPServer) Interval() time.Duration {
	if s.Spec.DiscoveryInterval == nil || s.Spec.DiscoveryInterval.Duration <= 0 {
		return DefaultDiscoveryInterval
	}
	return max(s.Spec.DiscoveryInterval.Duration, MinDiscoveryInterval)
}
