package controllers

import (
	"context"
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
