package wildflyserver

import (
	"context"
	"testing"
	"time"

	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	"github.com/wildfly/wildfly-operator/pkg/resources/services"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

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
	name             = "myapp"
	namespace        = "mynamespace"
	replicas         = int32(0)
	applicationImage = "my-app-image"
	sessionAffinity  = true
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
			SessionAffinity:  sessionAffinity,
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
	// statefulset will be created
	_, err := r.Reconcile(req)
	require.NoError(t, err)

	// Check if stateful set has been created and has the correct size.
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	assert.Equal(replicas, *statefulSet.Spec.Replicas)
	assert.Equal(applicationImage, statefulSet.Spec.Template.Spec.Containers[0].Image)

	// loadbalancer service will be created
	_, err = r.Reconcile(req)
	require.NoError(t, err)

	loadbalancer := &corev1.Service{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: services.LoadBalancerServiceName(wildflyServer), Namespace: req.Namespace}, loadbalancer)
	require.NoError(t, err)
	assert.Equal(corev1.ServiceAffinityClientIP, loadbalancer.Spec.SessionAffinity)

	// headless service will be created
	_, err = r.Reconcile(req)
	require.NoError(t, err)

	headlessService := &corev1.Service{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: services.HeadlessServiceName(wildflyServer), Namespace: req.Namespace}, headlessService)
	require.NoError(t, err)

}

func TestEnvUpdate(t *testing.T) {
	// Set the logger to development mode for verbose logs.
	logf.SetLogger(logf.ZapLogger(true))
	assert := assert.New(t)

	initialEnv := &corev1.EnvVar{
		Name:  "TEST_START",
		Value: "INITIAL",
	}

	// A WildFlyServer resource with metadata and spec.
	wildflyServer := &wildflyv1alpha1.WildFlyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Size:             0,
			SessionAffinity:  sessionAffinity,
			Env: []corev1.EnvVar{
				*initialEnv,
			},
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
	r := &ReconcileWildFlyServer{client: cl, scheme: s, isOpenShift: false, isWildFlyFinalizer: false}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
	// Creating StatefulSet
	res, err := r.Reconcile(req)
	require.NoError(t, err)
	// Creating Loadbalancer service
	res, err = r.Reconcile(req)
	require.NoError(t, err)
	// Creating Headless service
	res, err = r.Reconcile(req)
	require.NoError(t, err)

	// Check if stateful set has been created and has the correct env var
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	for _, env := range statefulSet.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "TEST_START" {
			assert.Equal("INITIAL", env.Value)
		}
	}

	// update the env in the WildFlyServerSpec
	wildflyServer.Spec.Env[0].Value = "UPDATE"
	wildflyServer.SetGeneration(wildflyServer.GetGeneration() + 1)
	err = cl.Update(context.TODO(), wildflyServer)
	t.Logf("WildFlyServerSpec generation %d", wildflyServer.GetGeneration())
	require.NoError(t, err)

	res, err = r.Reconcile(req)
	require.NoError(t, err)
	if !res.Requeue {
		t.Error("reconcile did not requeue request as expected")
	}

	// check that the statefulset env has been updated
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	for _, env := range statefulSet.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "TEST_START" {
			assert.Equal("UPDATE", env.Value)
		}
	}
	// remove the env from the WildFlyServerSpec
	wildflyServer.Spec.Env = []corev1.EnvVar{}
	wildflyServer.SetGeneration(wildflyServer.GetGeneration() + 1)
	err = cl.Update(context.TODO(), wildflyServer)
	t.Logf("WildFlyServerSpec generation %d", wildflyServer.GetGeneration())
	require.NoError(t, err)
	res, err = r.Reconcile(req)
	require.NoError(t, err)
	if !res.Requeue {
		t.Error("reconcile did not requeue request as expected")
	}
	// check that the statefulset env has been removed
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	for _, env := range statefulSet.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "TEST_START" {
			t.Error("TEST_START env var must be removed")
		}
	}

	// adding a new env to WildFlyServerSpec
	addedEnv := &corev1.EnvVar{
		Name:  "TEST_ADD",
		Value: "ADD",
	}
	wildflyServer.Spec.Env = []corev1.EnvVar{
		*addedEnv,
	}
	wildflyServer.SetGeneration(wildflyServer.GetGeneration() + 1)
	err = cl.Update(context.TODO(), wildflyServer)
	t.Logf("WildFlyServerSpec generation %d", wildflyServer.GetGeneration())
	require.NoError(t, err)

	res, err = r.Reconcile(req)
	require.NoError(t, err)
	if !res.Requeue {
		t.Error("reconcile did not requeue request as expected")
	}

	// check that the statefulset env has been added
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	for _, env := range statefulSet.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "TEST_ADD" {
			assert.Equal("ADD", env.Value)
		}
	}
}

func TestWildFlyServerControllerScaleDown(t *testing.T) {
	// Set the logger to development mode for verbose logs.
	logf.SetLogger(logf.ZapLogger(true))
	assert := assert.New(t)
	expectedReplicaSize := int32(1)

	// A WildFlyServer resource with metadata and spec.
	wildflyServer := &wildflyv1alpha1.WildFlyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Size:             expectedReplicaSize,
			SessionAffinity:  sessionAffinity,
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
	r := &ReconcileWildFlyServer{client: cl, scheme: s, recorder: eventRecorderMock{}, isOpenShift: false, isWildFlyFinalizer: true}
	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
	// Statefulset will be created
	_, err := r.Reconcile(req)
	require.NoError(t, err)

	// Check if stateful set has been created and has the correct size.
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	assert.Equal(expectedReplicaSize, *statefulSet.Spec.Replicas)
	assert.Equal(applicationImage, statefulSet.Spec.Template.Spec.Containers[0].Image)

	// Finalizer will be added
	_, err = r.Reconcile(req)
	require.NoError(t, err)
	err = cl.Get(context.TODO(), req.NamespacedName, wildflyServer)
	require.NoError(t, err)
	assert.Equal(1, len(wildflyServer.GetFinalizers()))
	assert.Equal("finalizer.wildfly.org", wildflyServer.GetFinalizers()[0])
	// Operator correctly setup the StatefulSet replica thus move on and create the Pod that the operator waits for
	//   StatefulSet won't do this here for us thus manual creation is needed
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ScaleDownTestPod", Namespace: wildflyServer.Namespace, Labels: LabelsForWildFly(wildflyServer)},
		TypeMeta:   metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"}}
	err = cl.Create(context.TODO(), pod)
	require.NoError(t, err)

	log.Info("Waiting for WildflyServer is updated to the state where WildflyServer.Status.Pods refers the Pod created by the test")
	err = wait.Poll(100*time.Millisecond, 5*time.Second, func() (done bool, err error) {
		_, err = r.Reconcile(req)
		require.NoError(t, err)

		podList, err := GetPodsForWildFly(r, wildflyServer)
		err2 := cl.Get(context.TODO(), req.NamespacedName, wildflyServer)
		if err == nil && len(podList.Items) == int(expectedReplicaSize) &&
			err2 == nil && len(wildflyServer.Status.Pods) == int(expectedReplicaSize) {
			return true, nil
		}
		return false, nil
	})

	log.Info("WildFly server was reconciliated to the state the pod status corresponds with namespace. Let's scale it down.",
		"WildflyServer", wildflyServer)
	assert.Equal(int(expectedReplicaSize), len(wildflyServer.Status.Pods))
	assert.Equal(wildflyv1alpha1.PodStateActive, wildflyServer.Status.Pods[0].State)
	wildflyServer.Spec.Size = 0
	err = cl.Update(context.TODO(), wildflyServer)

	// Reconcile for the scale down - updating the pod labels
	_, err = r.Reconcile(req)
	require.NoError(t, err)
	// Pod label has to be changed to not being active for service
	podList, err := GetPodsForWildFly(r, wildflyServer)
	require.NoError(t, err)
	assert.Equal(int(expectedReplicaSize), len(podList.Items))
	assert.Equal("disabled", podList.Items[0].GetLabels()["wildfly.org/operated-by-loadbalancer"])

	// Reconcile for the scale down - updating the pod state at the wildflyserver CR
	_, err = r.Reconcile(req) // error could be returned here as the scaledown was not sucessful here
	err = cl.Get(context.TODO(), req.NamespacedName, wildflyServer)
	require.NoError(t, err)
	assert.Equal(wildflyv1alpha1.PodStateScalingDownRecoveryInvestigation, wildflyServer.Status.Pods[0].State)
}

type eventRecorderMock struct {
}

func (rm eventRecorderMock) Event(object runtime.Object, eventtype, reason, message string) {}
func (rm eventRecorderMock) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
}
func (rm eventRecorderMock) PastEventf(object runtime.Object, timestamp metav1.Time, eventtype, reason, messageFmt string, args ...interface{}) {
}
func (rm eventRecorderMock) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
}
