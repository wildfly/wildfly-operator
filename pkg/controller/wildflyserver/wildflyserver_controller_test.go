package wildflyserver

import (
	"context"
	"os"
	"testing"

	"github.com/operator-framework/operator-sdk/pkg/log/zap"

	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	"github.com/wildfly/wildfly-operator/pkg/resources/services"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	testifyAssert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	name             = "myapp"
	namespace        = "mynamespace"
	replicas         = int32(0)
	applicationImage = "my-app-image"
	sessionAffinity  = true
)

func TestWildFlyServerControllerCreatesStatefulSet(t *testing.T) {
	// Set the loggclearer to development mode for verbose logs.
	logf.SetLogger(zap.Logger())
	assert := testifyAssert.New(t)

	// A WildFlyServer resource with metadata and spec.
	wildflyServer := &wildflyv1alpha1.WildFlyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Replicas:         replicas,
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
	// Check if the stateful set has the correct name label used by the HPA as a selector label.
	assert.Contains(statefulSet.Spec.Template.GetLabels()["app.kubernetes.io/name"], "myapp")

	// cluster service will be created
	_, err = r.Reconcile(req)
	require.NoError(t, err)

	clusterService := &corev1.Service{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: services.ClusterServiceName(wildflyServer), Namespace: req.Namespace}, clusterService)
	require.NoError(t, err)
	assert.Equal(corev1.ServiceAffinityClientIP, clusterService.Spec.SessionAffinity)

	// headless service will be created
	_, err = r.Reconcile(req)
	require.NoError(t, err)

	headlessService := &corev1.Service{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: services.HeadlessServiceName(wildflyServer), Namespace: req.Namespace}, headlessService)
	require.NoError(t, err)

}

func TestEnvUpdate(t *testing.T) {
	// Set the logger to development mode for verbose logs.
	logf.SetLogger(zap.Logger())
	assert := testifyAssert.New(t)

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
			Replicas:         0,
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
	r := &ReconcileWildFlyServer{client: cl, scheme: s, isOpenShift: false}

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

func TestWildFlyServerWithSecret(t *testing.T) {
	// Set the logger to development mode for verbose logs.
	logf.SetLogger(zap.Logger())
	assert := testifyAssert.New(t)

	secretName := "mysecret"
	secretKey := "my-key"
	secretValue := "my-very-secure-value"

	// A WildFlyServer resource with metadata and spec.
	wildflyServer := &wildflyv1alpha1.WildFlyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Replicas:         replicas,
			Secrets:          []string{secretName},
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

	err := cl.Create(context.TODO(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		StringData: map[string]string{
			secretKey: secretValue,
		},
	})
	require.NoError(t, err)

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
	// statefulset will be created
	_, err = r.Reconcile(req)
	require.NoError(t, err)

	// Check if stateful set has been created and has the correct size.
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	assert.Equal(replicas, *statefulSet.Spec.Replicas)
	assert.Equal(applicationImage, statefulSet.Spec.Template.Spec.Containers[0].Image)

	foundVolume := false
	for _, v := range statefulSet.Spec.Template.Spec.Volumes {
		if v.Name == "secret-"+secretName {
			source := v.VolumeSource
			if source.Secret.SecretName == secretName {
				foundVolume = true
				break
			}
		}
	}
	assert.True(foundVolume)

	foundVolumeMount := false
	for _, vm := range statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts {
		if vm.Name == "secret-"+secretName {
			assert.Equal("/etc/secrets/"+secretName, vm.MountPath)
			assert.True(vm.ReadOnly)
			foundVolumeMount = true
		}
	}
	assert.True(foundVolumeMount)
}

func TestWildFlyServerWithConfigMap(t *testing.T) {
	// Set the logger to development mode for verbose logs.
	logf.SetLogger(zap.Logger())
	assert := testifyAssert.New(t)

	configMapName := "my-config"

	// A WildFlyServer resource with metadata and spec.
	wildflyServer := &wildflyv1alpha1.WildFlyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Replicas:         replicas,
			ConfigMaps:       []string{configMapName},
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

	err := cl.Create(context.TODO(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: map[string]string{
			"key1": "value1",
		},
	})
	require.NoError(t, err)

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
	// statefulset will be created
	_, err = r.Reconcile(req)
	require.NoError(t, err)

	// Check if stateful set has been created and has the correct size.
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	assert.Equal(replicas, *statefulSet.Spec.Replicas)
	assert.Equal(applicationImage, statefulSet.Spec.Template.Spec.Containers[0].Image)

	foundVolume := false
	for _, v := range statefulSet.Spec.Template.Spec.Volumes {
		if v.Name == "configmap-"+configMapName {
			source := v.VolumeSource
			if source.ConfigMap.LocalObjectReference.Name == configMapName {
				foundVolume = true
				break
			}
		}
	}
	assert.True(foundVolume)

	foundVolumeMount := false
	for _, vm := range statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts {
		if vm.Name == "configmap-"+configMapName {
			assert.Equal("/etc/configmaps/"+configMapName, vm.MountPath)
			assert.True(vm.ReadOnly)
			foundVolumeMount = true
		}
	}
	assert.True(foundVolumeMount)
}

func TestWildFlyServerWithResources(t *testing.T) {
	// Set the logger to development mode for verbose logs.
	logf.SetLogger(zap.Logger())
	assert := testifyAssert.New(t)

	var (
		requestCpu = resource.MustParse("250m")
		requestMem = resource.MustParse("128Mi")
		limitCpu   = resource.MustParse("1")
		limitMem   = resource.MustParse("512Mi")
	)

	// A WildFlyServer resource with metadata and spec.
	wildflyServer := &wildflyv1alpha1.WildFlyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Replicas:         replicas,
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    requestCpu,
					corev1.ResourceMemory: requestMem,
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    limitCpu,
					corev1.ResourceMemory: limitMem,
				},
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

	// Check if stateful set has been created with the correct configuration.
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	assert.Equal(replicas, *statefulSet.Spec.Replicas)
	assert.Equal(applicationImage, statefulSet.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(requestCpu, statefulSet.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU])
	assert.Equal(requestMem, statefulSet.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory])
	assert.Equal(limitCpu, statefulSet.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU])
	assert.Equal(limitMem, statefulSet.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory])
}

func TestWildFlyServerWithHttpProbes(t *testing.T) {
	logf.SetLogger(zap.Logger())
	assert := testifyAssert.New(t)

	// First verify the default values are created when there is not
	// any Probe configuration
	wildflyServer := &wildflyv1alpha1.WildFlyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Replicas:         replicas,
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

	// Creating StatefulSet and loop waiting for reconcile to end
	_, err := reconcileUntilDone(t, r, req, 4)

	// Check if stateful set has been created and has the correct size.
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	assert.Equal(replicas, *statefulSet.Spec.Replicas)
	assert.Equal(applicationImage, statefulSet.Spec.Template.Spec.Containers[0].Image)

	// check the Probes values
	stsLivenessProbe := statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe
	assert.Equal(int32(60), stsLivenessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsLivenessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsLivenessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsLivenessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsLivenessProbe.FailureThreshold)
	assert.Equal("/health/live", stsLivenessProbe.HTTPGet.Path)
	assert.Equal("admin", stsLivenessProbe.HTTPGet.Port.StrVal)

	stsReadinessProbe := statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe
	assert.Equal(int32(10), stsReadinessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsReadinessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsReadinessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsReadinessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsReadinessProbe.FailureThreshold)
	assert.Equal("/health/ready", stsReadinessProbe.HTTPGet.Path)
	assert.Equal("admin", stsReadinessProbe.HTTPGet.Port.StrVal)

	// By default, Startup probe is not defined in the StatefulSet
	assert.Nil(statefulSet.Spec.Template.Spec.Containers[0].StartupProbe)

	// Update the CR configuring values for the Probes
	livenessProbe := &wildflyv1alpha1.ProbeSpec{
		InitialDelaySeconds: 10,
		TimeoutSeconds:      145,
		PeriodSeconds:       15,
		SuccessThreshold:    11,
		FailureThreshold:    19,
	}

	readinessProbe := &wildflyv1alpha1.ProbeSpec{
		InitialDelaySeconds: 20,
		TimeoutSeconds:      245,
		PeriodSeconds:       25,
		SuccessThreshold:    21,
		FailureThreshold:    29,
	}

	startupProbe := &wildflyv1alpha1.ProbeSpec{
		InitialDelaySeconds: 30,
		TimeoutSeconds:      345,
		PeriodSeconds:       35,
		SuccessThreshold:    31,
		FailureThreshold:    39,
	}
	wildflyServer.Spec.LivenessProbe = livenessProbe
	wildflyServer.Spec.ReadinessProbe = readinessProbe
	wildflyServer.Spec.StartupProbe = startupProbe

	wildflyServer.SetGeneration(wildflyServer.GetGeneration() + 1)
	err = cl.Update(context.TODO(), wildflyServer)
	t.Logf("WildFlyServerSpec generation %d", wildflyServer.GetGeneration())
	require.NoError(t, err)

	_, err = reconcileUntilDone(t, r, req, 10)

	statefulSet = &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)

	// check the Probes values
	stsLivenessProbe = statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe
	assert.Equal(int32(10), stsLivenessProbe.InitialDelaySeconds)
	assert.Equal(int32(145), stsLivenessProbe.TimeoutSeconds)
	assert.Equal(int32(15), stsLivenessProbe.PeriodSeconds)
	assert.Equal(int32(11), stsLivenessProbe.SuccessThreshold)
	assert.Equal(int32(19), stsLivenessProbe.FailureThreshold)
	assert.Equal("/health/live", stsLivenessProbe.HTTPGet.Path)
	assert.Equal("admin", stsLivenessProbe.HTTPGet.Port.StrVal)

	stsReadinessProbe = statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe
	assert.Equal(int32(20), stsReadinessProbe.InitialDelaySeconds)
	assert.Equal(int32(245), stsReadinessProbe.TimeoutSeconds)
	assert.Equal(int32(25), stsReadinessProbe.PeriodSeconds)
	assert.Equal(int32(21), stsReadinessProbe.SuccessThreshold)
	assert.Equal(int32(29), stsReadinessProbe.FailureThreshold)
	assert.Equal("/health/ready", stsReadinessProbe.HTTPGet.Path)
	assert.Equal("admin", stsReadinessProbe.HTTPGet.Port.StrVal)

	stsStartupProbe := statefulSet.Spec.Template.Spec.Containers[0].StartupProbe
	assert.Equal(int32(30), stsStartupProbe.InitialDelaySeconds)
	assert.Equal(int32(345), stsStartupProbe.TimeoutSeconds)
	assert.Equal(int32(35), stsStartupProbe.PeriodSeconds)
	assert.Equal(int32(31), stsStartupProbe.SuccessThreshold)
	assert.Equal(int32(39), stsStartupProbe.FailureThreshold)
	assert.Equal("/health/live", stsStartupProbe.HTTPGet.Path)
	assert.Equal("admin", stsStartupProbe.HTTPGet.Port.StrVal)
}

func TestWildFlyServerWithProbesScript(t *testing.T) {
	logf.SetLogger(zap.Logger())
	assert := testifyAssert.New(t)

	// First verify the default values are created when there is not
	// any Probe configuration
	wildflyServer := &wildflyv1alpha1.WildFlyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Replicas:         replicas,
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

	livenessScriptName := "test-liveness-script.sh"
	readinessScriptName := "test-readiness-script.sh"
	os.Setenv("SERVER_LIVENESS_SCRIPT", livenessScriptName)
	os.Setenv("SERVER_READINESS_SCRIPT", readinessScriptName)
	// Creating StatefulSet and loop waiting for reconcile to end
	_, err := reconcileUntilDone(t, r, req, 4)

	// Check if stateful set has been created and has the correct size.
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	assert.Equal(replicas, *statefulSet.Spec.Replicas)
	assert.Equal(applicationImage, statefulSet.Spec.Template.Spec.Containers[0].Image)

	// check the Probes values
	stsLivenessProbe := statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe
	assert.Equal(int32(60), stsLivenessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsLivenessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsLivenessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsLivenessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsLivenessProbe.FailureThreshold)
	assert.Nil(stsLivenessProbe.HTTPGet)
	assert.Equal("/bin/bash", stsLivenessProbe.Exec.Command[0])
	assert.Equal("-c", stsLivenessProbe.Exec.Command[1])
	assert.Equal("if [ -f 'test-liveness-script.sh' ]; then test-liveness-script.sh; else curl 127.0.0.1:9990/health/live; fi", stsLivenessProbe.Exec.Command[2])

	stsReadinessProbe := statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe
	assert.Equal(int32(10), stsReadinessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsReadinessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsReadinessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsReadinessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsReadinessProbe.FailureThreshold)
	assert.Nil(stsReadinessProbe.HTTPGet)
	assert.Equal("/bin/bash", stsReadinessProbe.Exec.Command[0])
	assert.Equal("-c", stsReadinessProbe.Exec.Command[1])
	assert.Equal("if [ -f 'test-readiness-script.sh' ]; then test-readiness-script.sh; else curl 127.0.0.1:9990/health/ready; fi", stsReadinessProbe.Exec.Command[2])

	assert.Nil(statefulSet.Spec.Template.Spec.Containers[0].StartupProbe)

	// Update the CR configuring values for the Probes
	livenessProbe := &wildflyv1alpha1.ProbeSpec{
		InitialDelaySeconds: 14,
		TimeoutSeconds:      158,
		PeriodSeconds:       14,
		SuccessThreshold:    13,
		FailureThreshold:    17,
	}

	readinessProbe := &wildflyv1alpha1.ProbeSpec{
		InitialDelaySeconds: 24,
		TimeoutSeconds:      258,
		PeriodSeconds:       24,
		SuccessThreshold:    23,
		FailureThreshold:    27,
	}

	startupProbe := &wildflyv1alpha1.ProbeSpec{
		InitialDelaySeconds: 34,
		TimeoutSeconds:      358,
		PeriodSeconds:       34,
		SuccessThreshold:    33,
		FailureThreshold:    37,
	}

	wildflyServer.Spec.LivenessProbe = livenessProbe
	wildflyServer.Spec.ReadinessProbe = readinessProbe
	wildflyServer.Spec.StartupProbe = startupProbe

	wildflyServer.SetGeneration(wildflyServer.GetGeneration() + 1)
	err = cl.Update(context.TODO(), wildflyServer)
	t.Logf("WildFlyServerSpec generation %d", wildflyServer.GetGeneration())
	require.NoError(t, err)

	_, err = reconcileUntilDone(t, r, req, 10)

	statefulSet = &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)

	// check the Probes values
	stsLivenessProbe = statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe
	assert.Equal(int32(14), stsLivenessProbe.InitialDelaySeconds)
	assert.Equal(int32(158), stsLivenessProbe.TimeoutSeconds)
	assert.Equal(int32(14), stsLivenessProbe.PeriodSeconds)
	assert.Equal(int32(13), stsLivenessProbe.SuccessThreshold)
	assert.Equal(int32(17), stsLivenessProbe.FailureThreshold)
	assert.Nil(stsLivenessProbe.HTTPGet)
	assert.Equal("/bin/bash", stsLivenessProbe.Exec.Command[0])
	assert.Equal("-c", stsLivenessProbe.Exec.Command[1])
	assert.Equal("if [ -f 'test-liveness-script.sh' ]; then test-liveness-script.sh; else curl 127.0.0.1:9990/health/live; fi", stsLivenessProbe.Exec.Command[2])

	stsReadinessProbe = statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe
	assert.Equal(int32(24), stsReadinessProbe.InitialDelaySeconds)
	assert.Equal(int32(258), stsReadinessProbe.TimeoutSeconds)
	assert.Equal(int32(24), stsReadinessProbe.PeriodSeconds)
	assert.Equal(int32(23), stsReadinessProbe.SuccessThreshold)
	assert.Equal(int32(27), stsReadinessProbe.FailureThreshold)
	assert.Nil(stsReadinessProbe.HTTPGet)
	assert.Equal("/bin/bash", stsReadinessProbe.Exec.Command[0])
	assert.Equal("-c", stsReadinessProbe.Exec.Command[1])
	assert.Equal("if [ -f 'test-readiness-script.sh' ]; then test-readiness-script.sh; else curl 127.0.0.1:9990/health/ready; fi", stsReadinessProbe.Exec.Command[2])

	stsStartupProbe := statefulSet.Spec.Template.Spec.Containers[0].StartupProbe
	assert.Equal(int32(34), stsStartupProbe.InitialDelaySeconds)
	assert.Equal(int32(358), stsStartupProbe.TimeoutSeconds)
	assert.Equal(int32(34), stsStartupProbe.PeriodSeconds)
	assert.Equal(int32(33), stsStartupProbe.SuccessThreshold)
	assert.Equal(int32(37), stsStartupProbe.FailureThreshold)
	assert.Nil(stsStartupProbe.HTTPGet)
	assert.Equal("/bin/bash", stsStartupProbe.Exec.Command[0])
	assert.Equal("-c", stsStartupProbe.Exec.Command[1])
	assert.Equal("if [ -f 'test-liveness-script.sh' ]; then test-liveness-script.sh; else curl 127.0.0.1:9990/health/live; fi", stsStartupProbe.Exec.Command[2])

	// Force HTTP GET instead of Exec probes
	livenessProbe = &wildflyv1alpha1.ProbeSpec{
		InitialDelaySeconds: 14,
		TimeoutSeconds:      158,
		PeriodSeconds:       14,
		SuccessThreshold:    13,
		FailureThreshold:    17,
		Http:                true,
	}

	readinessProbe = &wildflyv1alpha1.ProbeSpec{
		InitialDelaySeconds: 24,
		TimeoutSeconds:      258,
		PeriodSeconds:       24,
		SuccessThreshold:    23,
		FailureThreshold:    27,
		Http:                true,
	}

	startupProbe = &wildflyv1alpha1.ProbeSpec{
		InitialDelaySeconds: 34,
		TimeoutSeconds:      358,
		PeriodSeconds:       34,
		SuccessThreshold:    33,
		FailureThreshold:    37,
		Http:                true,
	}

	wildflyServer.Spec.LivenessProbe = livenessProbe
	wildflyServer.Spec.ReadinessProbe = readinessProbe
	wildflyServer.Spec.StartupProbe = startupProbe

	wildflyServer.SetGeneration(wildflyServer.GetGeneration() + 1)
	err = cl.Update(context.TODO(), wildflyServer)
	t.Logf("WildFlyServerSpec generation %d", wildflyServer.GetGeneration())
	require.NoError(t, err)

	_, err = reconcileUntilDone(t, r, req, 10)

	statefulSet = &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)

	// check the Probes values
	stsLivenessProbe = statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe
	assert.Equal(int32(14), stsLivenessProbe.InitialDelaySeconds)
	assert.Equal(int32(158), stsLivenessProbe.TimeoutSeconds)
	assert.Equal(int32(14), stsLivenessProbe.PeriodSeconds)
	assert.Equal(int32(13), stsLivenessProbe.SuccessThreshold)
	assert.Equal(int32(17), stsLivenessProbe.FailureThreshold)
	assert.Nil(stsLivenessProbe.Exec)
	assert.Equal("/health/live", stsLivenessProbe.HTTPGet.Path)
	assert.Equal("admin", stsLivenessProbe.HTTPGet.Port.StrVal)

	stsReadinessProbe = statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe
	assert.Equal(int32(24), stsReadinessProbe.InitialDelaySeconds)
	assert.Equal(int32(258), stsReadinessProbe.TimeoutSeconds)
	assert.Equal(int32(24), stsReadinessProbe.PeriodSeconds)
	assert.Equal(int32(23), stsReadinessProbe.SuccessThreshold)
	assert.Equal(int32(27), stsReadinessProbe.FailureThreshold)
	assert.Nil(stsReadinessProbe.Exec)
	assert.Equal("/health/ready", stsReadinessProbe.HTTPGet.Path)
	assert.Equal("admin", stsReadinessProbe.HTTPGet.Port.StrVal)

	stsStartupProbe = statefulSet.Spec.Template.Spec.Containers[0].StartupProbe
	assert.Equal(int32(34), stsStartupProbe.InitialDelaySeconds)
	assert.Equal(int32(358), stsStartupProbe.TimeoutSeconds)
	assert.Equal(int32(34), stsStartupProbe.PeriodSeconds)
	assert.Equal(int32(33), stsStartupProbe.SuccessThreshold)
	assert.Equal(int32(37), stsStartupProbe.FailureThreshold)
	assert.Nil(stsStartupProbe.Exec)
	assert.Equal("/health/live", stsStartupProbe.HTTPGet.Path)
	assert.Equal("admin", stsStartupProbe.HTTPGet.Port.StrVal)
}

func reconcileUntilDone(t *testing.T, r *ReconcileWildFlyServer, req reconcile.Request, maxLoop int) (reconcile.Result, error) {
	res, err := r.Reconcile(req)
	for max := 1; res.Requeue; max++ {
		require.NoError(t, err)
		if max > maxLoop {
			t.Error("Reconcile loop exceeded")
			t.FailNow()
		}
		res, err = r.Reconcile(req)
	}
	return res, err
}
