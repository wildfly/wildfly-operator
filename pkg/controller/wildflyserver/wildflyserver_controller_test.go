package wildflyserver

import (
	"context"
	"testing"

	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var (
	name                   = "wildfy-operator"
	namespace              = "wildfly"
	replicas         int32 = 3
	applicationImage       = "my-app-image"
)

func TestWildFlyServerControllerCreatesStatefulSet(t *testing.T) {
	// Set the logger to development mode for verbose logs.
	logf.SetLogger(logf.ZapLogger(true))
	assert := assert.New(t)

	// A WildFlyServer resource with metadata and spec.
	wildflyServer := &wildflyv1alpha1.WildFlyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Size:             replicas,
		},
	}
	// Objects to track in the fake client.
	objs := []runtime.Object{
		wildflyServer,
	}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(wildflyv1alpha1.SchemeGroupVersion, wildflyServer)
	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)
	// Create a ReconcileWildFlyServer object with the scheme and fake client.
	r := &ReconcileWildFlyServer{client: cl, scheme: s}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
	res, err := r.Reconcile(req)
	require.NoError(t, err)

	// Check the result of reconciliation to make sure it has the desired state.
	if !res.Requeue {
		t.Error("reconcile did not requeue request as expected")
	}

	// Check if stateful set has been created and has the correct size.
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)

	require.NoError(t, err)

	assert.Equal(replicas, *statefulSet.Spec.Replicas)
	assert.Equal(applicationImage, statefulSet.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(JBossUserID, *statefulSet.Spec.Template.Spec.SecurityContext.RunAsUser)
	assert.Equal(JBossGroupID, *statefulSet.Spec.Template.Spec.SecurityContext.RunAsGroup)
}
