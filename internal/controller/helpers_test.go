package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/discovery"
)

// newFakeClient builds an API client that knows the mcp-gate kinds and serves
// their status subresource, as the real one does.
func newFakeClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = gatev1alpha1.AddToScheme(scheme)
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&gatev1alpha1.MCPServer{}, &gatev1alpha1.MCPToolSet{}, &gatev1alpha1.MCPCapability{}, &gatev1alpha1.MCPGrant{}).
		Build()
}

func newTestRegistry(t *testing.T) *discovery.Registry {
	t.Helper()
	r := discovery.NewRegistry(discovery.Options{ConnectTimeout: 2 * time.Second})
	t.Cleanup(r.Close)
	return r
}

func reconcileNamed(t *testing.T, r interface {
	Reconcile(context.Context, ctrl.Request) (ctrl.Result, error)
}, name string) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: name}})
	require.NoError(t, err)
	return res
}

func get[T client.Object](t *testing.T, c client.Client, name string, obj T) T {
	t.Helper()
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: name}, obj))
	return obj
}

func condition(t *testing.T, conditions []metav1.Condition, conditionType string) metav1.Condition {
	t.Helper()
	c := meta.FindStatusCondition(conditions, conditionType)
	require.NotNil(t, c, "condition %s is not set", conditionType)
	return *c
}

func mcpServer(name, endpoint string) *gatev1alpha1.MCPServer {
	return &gatev1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Generation: 1},
		Spec:       gatev1alpha1.MCPServerSpec{Endpoint: endpoint},
	}
}

// readyServer is a server whose status says discovery works.
func readyServer(name string) *gatev1alpha1.MCPServer {
	s := mcpServer(name, "http://"+name+".invalid/mcp")
	s.Status.Available = true
	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type: gatev1alpha1.ConditionReady, Status: metav1.ConditionTrue, Reason: gatev1alpha1.ReasonDiscovered, ObservedGeneration: 1,
	})
	return s
}

func mcpToolSet(name, server string, tools ...string) *gatev1alpha1.MCPToolSet {
	return &gatev1alpha1.MCPToolSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Generation: 1},
		Spec:       gatev1alpha1.MCPToolSetSpec{ServerRef: server, Tools: tools},
	}
}

// withReady marks a resource Ready (or not) for its current generation, the
// way its own reconciler would have.
func withReady[T client.Object](obj T, ready bool) T {
	status := metav1.ConditionFalse
	reason := "SomethingIsWrong"
	if ready {
		status, reason = metav1.ConditionTrue, gatev1alpha1.ReasonAvailable
	}
	c := metav1.Condition{Type: gatev1alpha1.ConditionReady, Status: status, Reason: reason, ObservedGeneration: obj.GetGeneration()}
	switch o := any(obj).(type) {
	case *gatev1alpha1.MCPToolSet:
		meta.SetStatusCondition(&o.Status.Conditions, c)
	case *gatev1alpha1.MCPCapability:
		meta.SetStatusCondition(&o.Status.Conditions, c)
	}
	return obj
}

func mcpCapability(name string, toolSets ...string) *gatev1alpha1.MCPCapability {
	return &gatev1alpha1.MCPCapability{
		ObjectMeta: metav1.ObjectMeta{Name: name, Generation: 1},
		Spec:       gatev1alpha1.MCPCapabilitySpec{ToolSets: toolSets},
	}
}

func mcpGrant(name, subject string, capabilities ...string) *gatev1alpha1.MCPGrant {
	return &gatev1alpha1.MCPGrant{
		ObjectMeta: metav1.ObjectMeta{Name: name, Generation: 1},
		Spec:       gatev1alpha1.MCPGrantSpec{Subject: subject, Capabilities: capabilities},
	}
}
