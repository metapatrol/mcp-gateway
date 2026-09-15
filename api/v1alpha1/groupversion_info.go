// Package v1alpha1 contains API Schema definitions for the mcp-gateway.metapatrol.com API group.
// +kubebuilder:object:generate=true
// +groupName=mcp-gateway.metapatrol.com
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "mcp-gateway.metapatrol.com", Version: "v1alpha1"}

	// SchemeBuilder collects the functions that add this group's types to a
	// scheme. It is apimachinery's builder rather than controller-runtime's,
	// whose scheme.Builder is deprecated upstream so that an api package stays
	// cheap to import — which matters here, because the gateway imports these
	// types and nothing else of the controller.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&MCPServer{}, &MCPServerList{},
		&MCPToolSet{}, &MCPToolSetList{},
		&MCPCapability{}, &MCPCapabilityList{},
		&MCPGrant{}, &MCPGrantList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
