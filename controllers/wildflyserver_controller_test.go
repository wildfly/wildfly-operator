package controllers

import (
	"context"
	"k8s.io/apimachinery/pkg/util/intstr"
	"os"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"testing"

	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/api/v1alpha1"
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
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
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
	s.AddKnownTypes(wildflyv1alpha1.GroupVersion, wildflyServer)
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	// Create a WildFlyServerReconciler object with the scheme and fake client.
	r := &WildFlyServerReconciler{
		Client: cl,
		Scheme: s,
		Log:    ctrl.Log.WithName("test").WithName("WildFlyServer"),
	}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
	// statefulset will be created
	_, err := r.Reconcile(context.TODO(), req)
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
	_, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)

	clusterService := &corev1.Service{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: services.ClusterServiceName(wildflyServer), Namespace: req.Namespace}, clusterService)
	require.NoError(t, err)
	assert.Equal(corev1.ServiceAffinityClientIP, clusterService.Spec.SessionAffinity)

	// headless service will be created
	_, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)

	headlessService := &corev1.Service{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: services.HeadlessServiceName(wildflyServer), Namespace: req.Namespace}, headlessService)
	require.NoError(t, err)

}

func TestEnvUpdate(t *testing.T) {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	log := ctrl.Log.WithName("TestEnvUpdate")
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
	s.AddKnownTypes(wildflyv1alpha1.GroupVersion, wildflyServer)
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	// Create a WildFlyServerReconciler object with the scheme and fake client.
	r := &WildFlyServerReconciler{
		Client:      cl,
		Scheme:      s,
		IsOpenShift: false,
		Log:         ctrl.Log.WithName("test").WithName("WildFlyServer"),
	}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
	// Creating StatefulSet
	res, err := r.Reconcile(context.TODO(), req)
	require.NoError(t, err)
	// Creating Loadbalancer service
	res, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)
	// Creating Headless service
	res, err = r.Reconcile(context.TODO(), req)
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
	log.Info("WildFlyServerSpec generation", "Generation", wildflyServer.GetGeneration())
	require.NoError(t, err)

	res, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)
	if !res.Requeue {
		log.Error(nil, "reconcile did not requeue request as expected")
		t.FailNow()
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
	log.Info("WildFlyServerSpec generation", "Generation", wildflyServer.GetGeneration())
	require.NoError(t, err)
	res, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)
	if !res.Requeue {
		log.Error(nil, "reconcile did not requeue request as expected")
		t.FailNow()
	}
	// check that the statefulset env has been removed
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	for _, env := range statefulSet.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "TEST_START" {
			log.Error(nil, "TEST_START env var must be removed")
			t.FailNow()
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
	log.Info("WildFlyServerSpec generation", "Generation", wildflyServer.GetGeneration())
	require.NoError(t, err)

	res, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)
	if !res.Requeue {
		log.Error(nil, "reconcile did not requeue request as expected")
		t.FailNow()
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
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
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
	s.AddKnownTypes(wildflyv1alpha1.GroupVersion, wildflyServer)
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	// Create a WildFlyServerReconciler object with the scheme and fake client.
	r := &WildFlyServerReconciler{
		Client: cl,
		Scheme: s,
		Log:    ctrl.Log.WithName("test").WithName("WildFlyServer"),
	}

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
	_, err = r.Reconcile(context.TODO(), req)
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
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
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
	s.AddKnownTypes(wildflyv1alpha1.GroupVersion, wildflyServer)
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	// Create a WildFlyServerReconciler object with the scheme and fake client.
	r := &WildFlyServerReconciler{
		Client: cl,
		Scheme: s,
		Log:    ctrl.Log.WithName("test").WithName("WildFlyServer"),
	}

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
	_, err = r.Reconcile(context.TODO(), req)
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
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
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
	s.AddKnownTypes(wildflyv1alpha1.GroupVersion, wildflyServer)
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	// Create a WildFlyServerReconciler object with the scheme and fake client.
	r := &WildFlyServerReconciler{
		Client: cl,
		Scheme: s,
		Log:    ctrl.Log.WithName("test").WithName("WildFlyServer"),
	}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
	// statefulset will be created
	_, err := r.Reconcile(context.TODO(), req)
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

func TestWildFlyServerWithSecurityContext(t *testing.T) {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	assert := testifyAssert.New(t)

	allowPrivilegeEscalation := new(bool)
	*allowPrivilegeEscalation = false
	privileged := new(bool)
	*privileged = false
	readOnlyRootFilesystem := new(bool)
	*readOnlyRootFilesystem = true
	runAsNonRoot := new(bool)
	*runAsNonRoot = true

	var (
		capabilities = &corev1.Capabilities{
			Drop: []corev1.Capability{
				"ALL",
			},
		}
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
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: allowPrivilegeEscalation,
				Capabilities:             capabilities,
				Privileged:               privileged,
				ReadOnlyRootFilesystem:   readOnlyRootFilesystem,
				RunAsNonRoot:             runAsNonRoot,
			},
		},
	}
	// Objects to track in the fake client.
	objs := []runtime.Object{
		wildflyServer,
	}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(wildflyv1alpha1.GroupVersion, wildflyServer)
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	// Create a WildFlyServerReconciler object with the scheme and fake client.
	r := &WildFlyServerReconciler{
		Client: cl,
		Scheme: s,
		Log:    ctrl.Log.WithName("test").WithName("WildFlyServer"),
	}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
	// statefulset will be created
	_, err := r.Reconcile(context.TODO(), req)
	require.NoError(t, err)

	// Check if stateful set has been created with the correct configuration.
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	assert.Equal(replicas, *statefulSet.Spec.Replicas)
	assert.Equal(applicationImage, statefulSet.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(allowPrivilegeEscalation, statefulSet.Spec.Template.Spec.Containers[0].SecurityContext.AllowPrivilegeEscalation)
	assert.Equal(capabilities.Drop[0], statefulSet.Spec.Template.Spec.Containers[0].SecurityContext.Capabilities.Drop[0])
	assert.Equal(privileged, statefulSet.Spec.Template.Spec.Containers[0].SecurityContext.Privileged)
	assert.Equal(readOnlyRootFilesystem, statefulSet.Spec.Template.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem)
	assert.Equal(runAsNonRoot, statefulSet.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot)
}

func TestWildFlyServerWithDefaultHttpProbes(t *testing.T) {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
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
	s.AddKnownTypes(wildflyv1alpha1.GroupVersion, wildflyServer)

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	// Create a WildFlyServerReconciler object with the scheme and fake client.
	r := &WildFlyServerReconciler{
		Client: cl,
		Scheme: s,
		Log:    ctrl.Log.WithName("test").WithName("WildFlyServer"),
	}

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
	assert.Equal(int32(0), stsLivenessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsLivenessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsLivenessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsLivenessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsLivenessProbe.FailureThreshold)
	assert.Equal("/health/live", stsLivenessProbe.HTTPGet.Path)
	assert.Equal("admin", stsLivenessProbe.HTTPGet.Port.StrVal)

	stsReadinessProbe := statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe
	assert.Equal(int32(0), stsReadinessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsReadinessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsReadinessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsReadinessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsReadinessProbe.FailureThreshold)
	assert.Equal("/health/ready", stsReadinessProbe.HTTPGet.Path)
	assert.Equal("admin", stsReadinessProbe.HTTPGet.Port.StrVal)

	// By default, Startup probe is not defined in the StatefulSet
	assert.Nil(statefulSet.Spec.Template.Spec.Containers[0].StartupProbe)

	// Update the CR adding just an StartupProbe Configuration
	startupProbe := &wildflyv1alpha1.ProbeSpec{
		InitialDelaySeconds: 65,
		TimeoutSeconds:      55,
		PeriodSeconds:       75,
		SuccessThreshold:    85,
		FailureThreshold:    10,
	}

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
	assert.Equal(int32(0), stsLivenessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsLivenessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsLivenessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsLivenessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsLivenessProbe.FailureThreshold)
	assert.Equal("/health/live", stsLivenessProbe.HTTPGet.Path)
	assert.Equal("admin", stsLivenessProbe.HTTPGet.Port.StrVal)

	stsReadinessProbe = statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe
	assert.Equal(int32(0), stsReadinessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsReadinessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsReadinessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsReadinessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsReadinessProbe.FailureThreshold)
	assert.Equal("/health/ready", stsReadinessProbe.HTTPGet.Path)
	assert.Equal("admin", stsReadinessProbe.HTTPGet.Port.StrVal)

	stsStartupProbe := statefulSet.Spec.Template.Spec.Containers[0].StartupProbe
	assert.Equal(int32(65), stsStartupProbe.InitialDelaySeconds)
	assert.Equal(int32(55), stsStartupProbe.TimeoutSeconds)
	assert.Equal(int32(75), stsStartupProbe.PeriodSeconds)
	assert.Equal(int32(85), stsStartupProbe.SuccessThreshold)
	assert.Equal(int32(10), stsStartupProbe.FailureThreshold)
	assert.Equal("/health/live", stsStartupProbe.HTTPGet.Path)
	assert.Equal("admin", stsStartupProbe.HTTPGet.Port.StrVal)

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

	startupProbe = &wildflyv1alpha1.ProbeSpec{
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

	stsStartupProbe = statefulSet.Spec.Template.Spec.Containers[0].StartupProbe
	assert.Equal(int32(30), stsStartupProbe.InitialDelaySeconds)
	assert.Equal(int32(345), stsStartupProbe.TimeoutSeconds)
	assert.Equal(int32(35), stsStartupProbe.PeriodSeconds)
	assert.Equal(int32(31), stsStartupProbe.SuccessThreshold)
	assert.Equal(int32(39), stsStartupProbe.FailureThreshold)
	assert.Equal("/health/live", stsStartupProbe.HTTPGet.Path)
	assert.Equal("admin", stsStartupProbe.HTTPGet.Port.StrVal)
}

func TestWildFlyServerWithDefaultProbesScript(t *testing.T) {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	assert := testifyAssert.New(t)

	livenessScriptName := "test-liveness-script.sh"
	readinessScriptName := "test-readiness-script.sh"
	os.Setenv("SERVER_LIVENESS_SCRIPT", livenessScriptName)
	os.Setenv("SERVER_READINESS_SCRIPT", readinessScriptName)

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
	s.AddKnownTypes(wildflyv1alpha1.GroupVersion, wildflyServer)

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	// Create a WildFlyServerReconciler object with the scheme and fake client.
	r := &WildFlyServerReconciler{
		Client: cl,
		Scheme: s,
		Log:    ctrl.Log.WithName("test").WithName("WildFlyServer"),
	}

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
	assert.Equal(int32(0), stsLivenessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsLivenessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsLivenessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsLivenessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsLivenessProbe.FailureThreshold)
	assert.Nil(stsLivenessProbe.HTTPGet)
	assert.Equal("/bin/bash", stsLivenessProbe.Exec.Command[0])
	assert.Equal("-c", stsLivenessProbe.Exec.Command[1])
	assert.Equal("if [ -f 'test-liveness-script.sh' ]; then test-liveness-script.sh; else curl --fail http://127.0.0.1:9990/health/live; fi", stsLivenessProbe.Exec.Command[2])

	stsReadinessProbe := statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe
	assert.Equal(int32(0), stsReadinessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsReadinessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsReadinessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsReadinessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsReadinessProbe.FailureThreshold)
	assert.Nil(stsReadinessProbe.HTTPGet)
	assert.Equal("/bin/bash", stsReadinessProbe.Exec.Command[0])
	assert.Equal("-c", stsReadinessProbe.Exec.Command[1])
	assert.Equal("if [ -f 'test-readiness-script.sh' ]; then test-readiness-script.sh; else curl --fail http://127.0.0.1:9990/health/ready; fi", stsReadinessProbe.Exec.Command[2])

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
	assert.Equal("if [ -f 'test-liveness-script.sh' ]; then test-liveness-script.sh; else curl --fail http://127.0.0.1:9990/health/live; fi", stsLivenessProbe.Exec.Command[2])

	stsReadinessProbe = statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe
	assert.Equal(int32(24), stsReadinessProbe.InitialDelaySeconds)
	assert.Equal(int32(258), stsReadinessProbe.TimeoutSeconds)
	assert.Equal(int32(24), stsReadinessProbe.PeriodSeconds)
	assert.Equal(int32(23), stsReadinessProbe.SuccessThreshold)
	assert.Equal(int32(27), stsReadinessProbe.FailureThreshold)
	assert.Nil(stsReadinessProbe.HTTPGet)
	assert.Equal("/bin/bash", stsReadinessProbe.Exec.Command[0])
	assert.Equal("-c", stsReadinessProbe.Exec.Command[1])
	assert.Equal("if [ -f 'test-readiness-script.sh' ]; then test-readiness-script.sh; else curl --fail http://127.0.0.1:9990/health/ready; fi", stsReadinessProbe.Exec.Command[2])

	stsStartupProbe := statefulSet.Spec.Template.Spec.Containers[0].StartupProbe
	assert.Equal(int32(34), stsStartupProbe.InitialDelaySeconds)
	assert.Equal(int32(358), stsStartupProbe.TimeoutSeconds)
	assert.Equal(int32(34), stsStartupProbe.PeriodSeconds)
	assert.Equal(int32(33), stsStartupProbe.SuccessThreshold)
	assert.Equal(int32(37), stsStartupProbe.FailureThreshold)
	assert.Nil(stsStartupProbe.HTTPGet)
	assert.Equal("/bin/bash", stsStartupProbe.Exec.Command[0])
	assert.Equal("-c", stsStartupProbe.Exec.Command[1])
	assert.Equal("if [ -f 'test-liveness-script.sh' ]; then test-liveness-script.sh; else curl --fail http://127.0.0.1:9990/health/live; fi", stsStartupProbe.Exec.Command[2])
}

func TestWildFlyServerWithExplicitHttpGetProbes(t *testing.T) {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	assert := testifyAssert.New(t)

	livenessScriptName := "test-liveness-script.sh"
	readinessScriptName := "test-readiness-script.sh"
	os.Setenv("SERVER_LIVENESS_SCRIPT", livenessScriptName)
	os.Setenv("SERVER_READINESS_SCRIPT", readinessScriptName)

	livenessHeaders := []corev1.HTTPHeader{
		{
			Name:  "h-name-liveness-1",
			Value: "h-value-liveness-1",
		},
		{
			Name:  "h-name-liveness-2",
			Value: "h-value-liveness-2",
		},
	}

	livenessProbe := &wildflyv1alpha1.ProbeSpec{
		ProbeHandler: wildflyv1alpha1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:        "/test/liveness",
				Port:        intstr.FromString("0"),
				Host:        "host-liveness",
				Scheme:      "HTTP",
				HTTPHeaders: livenessHeaders,
			},
		},
	}

	readinessHeaders := []corev1.HTTPHeader{
		{
			Name:  "h-name-readiness-1",
			Value: "h-value-readiness-1",
		},
		{
			Name:  "h-name-readiness-2",
			Value: "h-value-readiness-2",
		},
	}
	readinessProbe := &wildflyv1alpha1.ProbeSpec{
		ProbeHandler: wildflyv1alpha1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:        "/test/readiness",
				Port:        intstr.FromString("1"),
				Host:        "host-readiness",
				Scheme:      "HTTP",
				HTTPHeaders: readinessHeaders,
			},
		},
	}

	startupHeaders := []corev1.HTTPHeader{
		{
			Name:  "h-name-startup-1",
			Value: "h-value-startup-1",
		},
		{
			Name:  "h-name-startup-2",
			Value: "h-value-startup-2",
		},
	}
	startupProbe := &wildflyv1alpha1.ProbeSpec{
		ProbeHandler: wildflyv1alpha1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:        "/test/startup",
				Port:        intstr.FromString("2"),
				Host:        "host-startup",
				Scheme:      "HTTPS",
				HTTPHeaders: startupHeaders,
			},
		},
	}

	wildflyServer := &wildflyv1alpha1.WildFlyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Replicas:         replicas,
			LivenessProbe:    livenessProbe,
			ReadinessProbe:   readinessProbe,
			StartupProbe:     startupProbe,
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{
		wildflyServer,
	}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(wildflyv1alpha1.GroupVersion, wildflyServer)

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	// Create a WildFlyServerReconciler object with the scheme and fake client.
	r := &WildFlyServerReconciler{
		Client: cl,
		Scheme: s,
		Log:    ctrl.Log.WithName("test").WithName("WildFlyServer"),
	}

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
	assert.Equal(int32(0), stsLivenessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsLivenessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsLivenessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsLivenessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsLivenessProbe.FailureThreshold)
	assert.Equal("/test/liveness", stsLivenessProbe.HTTPGet.Path)
	assert.Equal("0", stsLivenessProbe.HTTPGet.Port.StrVal)
	assert.Equal("host-liveness", stsLivenessProbe.HTTPGet.Host)
	assert.Equal(corev1.URIScheme("HTTP"), stsLivenessProbe.HTTPGet.Scheme)
	assert.True(reflect.DeepEqual(livenessHeaders, stsLivenessProbe.HTTPGet.HTTPHeaders))
	assert.Nil(stsLivenessProbe.Exec)

	stsReadinessProbe := statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe
	assert.Equal(int32(0), stsReadinessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsReadinessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsReadinessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsReadinessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsReadinessProbe.FailureThreshold)
	assert.Equal("/test/readiness", stsReadinessProbe.HTTPGet.Path)
	assert.Equal("1", stsReadinessProbe.HTTPGet.Port.StrVal)
	assert.Equal("host-readiness", stsReadinessProbe.HTTPGet.Host)
	assert.Equal(corev1.URIScheme("HTTP"), stsReadinessProbe.HTTPGet.Scheme)
	assert.True(reflect.DeepEqual(readinessHeaders, stsReadinessProbe.HTTPGet.HTTPHeaders))
	assert.Nil(stsReadinessProbe.Exec)

	stsStartupProbe := statefulSet.Spec.Template.Spec.Containers[0].StartupProbe
	assert.Equal(int32(5), stsStartupProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsStartupProbe.TimeoutSeconds)
	assert.Equal(int32(5), stsStartupProbe.PeriodSeconds)
	assert.Equal(int32(0), stsStartupProbe.SuccessThreshold)
	assert.Equal(int32(36), stsStartupProbe.FailureThreshold)
	assert.Equal("/test/startup", stsStartupProbe.HTTPGet.Path)
	assert.Equal("2", stsStartupProbe.HTTPGet.Port.StrVal)
	assert.Equal("host-startup", stsStartupProbe.HTTPGet.Host)
	assert.Equal(corev1.URIScheme("HTTPS"), stsStartupProbe.HTTPGet.Scheme)
	assert.True(reflect.DeepEqual(startupHeaders, stsStartupProbe.HTTPGet.HTTPHeaders))
	assert.Nil(stsStartupProbe.Exec)
}

func TestWildFlyServerWithExplicitHttpExecProbes(t *testing.T) {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	assert := testifyAssert.New(t)

	livenessScriptName := "test-liveness-script.sh"
	readinessScriptName := "test-readiness-script.sh"
	os.Setenv("SERVER_LIVENESS_SCRIPT", livenessScriptName)
	os.Setenv("SERVER_READINESS_SCRIPT", readinessScriptName)

	livenessExec := []string{
		"/bin/sh",
		"-c",
		"exec liveness probe",
	}
	livenessProbe := &wildflyv1alpha1.ProbeSpec{
		ProbeHandler: wildflyv1alpha1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: livenessExec,
			},
		},
	}

	readinessExec := []string{
		"/bin/sh",
		"-c",
		"exec readiness probe",
	}
	readinessProbe := &wildflyv1alpha1.ProbeSpec{
		ProbeHandler: wildflyv1alpha1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: readinessExec,
			},
		},
	}

	startupExec := []string{
		"/bin/sh",
		"-c",
		"exec readiness probe",
	}
	startupProbe := &wildflyv1alpha1.ProbeSpec{
		ProbeHandler: wildflyv1alpha1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: startupExec,
			},
		},
	}

	wildflyServer := &wildflyv1alpha1.WildFlyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Replicas:         replicas,
			LivenessProbe:    livenessProbe,
			ReadinessProbe:   readinessProbe,
			StartupProbe:     startupProbe,
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{
		wildflyServer,
	}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(wildflyv1alpha1.GroupVersion, wildflyServer)

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	// Create a WildFlyServerReconciler object with the scheme and fake client.
	r := &WildFlyServerReconciler{
		Client: cl,
		Scheme: s,
		Log:    ctrl.Log.WithName("test").WithName("WildFlyServer"),
	}

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
	assert.Equal(int32(0), stsLivenessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsLivenessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsLivenessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsLivenessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsLivenessProbe.FailureThreshold)
	assert.True(reflect.DeepEqual(livenessExec, stsLivenessProbe.Exec.Command))
	assert.Nil(stsLivenessProbe.HTTPGet)

	stsReadinessProbe := statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe
	assert.Equal(int32(0), stsReadinessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsReadinessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsReadinessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsReadinessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsReadinessProbe.FailureThreshold)
	assert.True(reflect.DeepEqual(readinessExec, stsReadinessProbe.Exec.Command))
	assert.Nil(stsReadinessProbe.HTTPGet)

	stsStartupProbe := statefulSet.Spec.Template.Spec.Containers[0].StartupProbe
	assert.Equal(int32(5), stsStartupProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsStartupProbe.TimeoutSeconds)
	assert.Equal(int32(5), stsStartupProbe.PeriodSeconds)
	assert.Equal(int32(0), stsStartupProbe.SuccessThreshold)
	assert.Equal(int32(36), stsStartupProbe.FailureThreshold)
	assert.True(reflect.DeepEqual(startupExec, stsStartupProbe.Exec.Command))
	assert.Nil(stsStartupProbe.HTTPGet)
}

func TestWildFlyServerWithExecAndHttpProbes(t *testing.T) {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	assert := testifyAssert.New(t)

	livenessScriptName := "test-liveness-script.sh"
	readinessScriptName := "test-readiness-script.sh"
	os.Setenv("SERVER_LIVENESS_SCRIPT", livenessScriptName)
	os.Setenv("SERVER_READINESS_SCRIPT", readinessScriptName)

	livenessExec := &corev1.ExecAction{
		Command: []string{
			"/bin/sh",
			"-c",
			"exec liveness probe",
		},
	}

	livenessHttp := &corev1.HTTPGetAction{
		Path:   "/test/liveness",
		Port:   intstr.FromString("0"),
		Host:   "host-liveness",
		Scheme: "HTTP",
	}

	livenessProbe := &wildflyv1alpha1.ProbeSpec{
		ProbeHandler: wildflyv1alpha1.ProbeHandler{
			Exec:    livenessExec,
			HTTPGet: livenessHttp,
		},
	}

	wildflyServer := &wildflyv1alpha1.WildFlyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Replicas:         replicas,
			LivenessProbe:    livenessProbe,
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{
		wildflyServer,
	}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(wildflyv1alpha1.GroupVersion, wildflyServer)

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	// Create a WildFlyServerReconciler object with the scheme and fake client.
	r := &WildFlyServerReconciler{
		Client: cl,
		Scheme: s,
		Log:    ctrl.Log.WithName("test").WithName("WildFlyServer"),
	}

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

	// check the Probes values. Exec should win against http
	stsLivenessProbe := statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe
	assert.Equal(int32(0), stsLivenessProbe.InitialDelaySeconds)
	assert.Equal(int32(0), stsLivenessProbe.TimeoutSeconds)
	assert.Equal(int32(0), stsLivenessProbe.PeriodSeconds)
	assert.Equal(int32(0), stsLivenessProbe.SuccessThreshold)
	assert.Equal(int32(0), stsLivenessProbe.FailureThreshold)
	assert.True(reflect.DeepEqual(livenessExec.Command, stsLivenessProbe.Exec.Command))
	assert.Nil(stsLivenessProbe.HTTPGet)
}

func reconcileUntilDone(t *testing.T, r *WildFlyServerReconciler, req reconcile.Request, maxLoop int) (ctrl.Result, error) {
	res, err := r.Reconcile(context.TODO(), req)
	for max := 1; res.Requeue; max++ {
		require.NoError(t, err)
		if max > maxLoop {
			t.Error("Reconcile loop exceeded")
			t.FailNow()
		}
		res, err = r.Reconcile(context.TODO(), req)
	}
	return res, err
}
