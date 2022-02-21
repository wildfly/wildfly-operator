package controllers

import (
	"context"
	"fmt"
	"github.com/prometheus/common/log"
	testifyAssert "github.com/stretchr/testify/assert"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/api/v1alpha1"
	wildflyutil "github.com/wildfly/wildfly-operator/pkg/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"regexp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	recoveryTestName      = "recoverytest"
	recoveryTestNameSpace = "recoverytest-namespace"
	recoveryTestAppImage  = "recoverytest-image"
)

var (
	// variables to be setup and re-used in the method over the file
	assert *testifyAssert.Assertions
	cl     client.Client
	r      *WildFlyServerReconciler

	// Mock request to simulate Reconcile() being called on an event for a watched resource.
	req = reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      recoveryTestName,
			Namespace: recoveryTestNameSpace,
		},
	}

	// A WildFlyServer resource with metadata and spec.
	//  expectedReplicaSize when this is used is 1
	defaultWildflyServerDefinition = wildflyv1alpha1.WildFlyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      recoveryTestName,
			Namespace: recoveryTestNameSpace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: recoveryTestAppImage,
			Replicas:         1,
			SessionAffinity:  true,
			Storage: &wildflyv1alpha1.StorageSpec{
				VolumeClaimTemplate: corev1.PersistentVolumeClaim{
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceRequestsStorage: resource.MustParse("3Gi"),
							},
						},
					},
				},
			},
		},
	}
)

func setupBeforeScaleDown(t *testing.T, wildflyServer *wildflyv1alpha1.WildFlyServer, expectedReplicaSize int32) {
	// Set the logger to development mode for verbose logs.
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	assert = testifyAssert.New(t)

	// Objects to track in the fake client.
	objs := []runtime.Object{
		wildflyServer,
	}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(wildflyv1alpha1.GroupVersion, wildflyServer)
	// Create a fake client to mock API calls.
	cl = fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()
	// Create a WildFlyServerReconciler object with the scheme and fake client.
	r = &WildFlyServerReconciler{Client: cl,
		Scheme:      s,
		Recorder:    eventRecorderMock{},
		IsOpenShift: false,
		Log:         ctrl.Log.WithName("test").WithName("transaction"),
	}

	// Statefulset will be created
	_, err := r.Reconcile(context.TODO(), req)
	require.NoError(t, err)

	// Check if stateful set has been created and has the correct size.
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	require.NoError(t, err)
	assert.Equal(expectedReplicaSize, *statefulSet.Spec.Replicas)
	assert.Equal(recoveryTestAppImage, statefulSet.Spec.Template.Spec.Containers[0].Image)

	// Operator correctly setup the StatefulSet replica thus move on and create the Pod that the operator waits for
	//   StatefulSet won't do this here for us thus manual creation is needed
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ScaleDownTestPod", Namespace: wildflyServer.Namespace, Labels: LabelsForWildFly(wildflyServer)},
		TypeMeta:   metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	err = cl.Create(context.TODO(), pod)
	require.NoError(t, err)

	// Emulate the statefulset started the pod and thus changed its own status as well
	statefulSet.Status.Replicas = expectedReplicaSize
	err = cl.Update(context.TODO(), statefulSet)
	require.NoError(t, err)

	log.Info("Waiting for WildflyServer is updated to the state where WildflyServer.Status.Pods refers the Pod created by the test")
	err = wait.Poll(100*time.Millisecond, 5*time.Second, func() (done bool, err error) {
		_, err = r.Reconcile(context.TODO(), req)
		require.NoError(t, err)

		podList, err := GetPodsForWildFly(r, wildflyServer)
		err2 := cl.Get(context.TODO(), req.NamespacedName, wildflyServer)
		if err == nil && len(podList.Items) == int(expectedReplicaSize) &&
			err2 == nil && len(wildflyServer.Status.Pods) == int(expectedReplicaSize) &&
			wildflyServer.Status.Replicas == expectedReplicaSize {
			return true, nil
		}
		return false, nil
	})
	require.NoError(t, err)

	assert.Equal(int(expectedReplicaSize), len(wildflyServer.Status.Pods))
	assert.Equal(wildflyv1alpha1.PodStateActive, wildflyServer.Status.Pods[0].State)
	assert.Equal(int32(0), wildflyServer.Status.ScalingdownPods)
	assert.Equal(expectedReplicaSize, wildflyServer.Status.Replicas)
}

func TestRecoveryScaleDownToPodInvestigation(t *testing.T) {
	wildflyServer := defaultWildflyServerDefinition.DeepCopy()
	expectedReplicaSize := int32(1)

	setupBeforeScaleDown(t, wildflyServer, expectedReplicaSize)

	log.Info("WildFly server was reconciled to the state the pod status corresponds with namespace. Let's scale it down.",
		"WildflyServer", wildflyServer)
	wildflyServer.Spec.Replicas = 0
	err := cl.Update(context.TODO(), wildflyServer)
	require.NoError(t, err)

	// Reconcile for the scale down - updating the pod labels
	_, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)
	// Pod label has to be changed to not being active for service
	podList, err := GetPodsForWildFly(r, wildflyServer)
	require.NoError(t, err)
	assert.Equal(int(expectedReplicaSize), len(podList.Items))
	assert.Equal("disabled", podList.Items[0].GetLabels()["wildfly.org/operated-by-loadbalancer"])
	assert.Equal(int32(0), wildflyServer.Status.ScalingdownPods) // scaledown in processing in next cycle
	assert.Equal(int32(1), wildflyServer.Status.Replicas)        // but real number of replicas has not been changed yet

	// Mocking the jboss-cli.sh calls to reach the loop on working with pods at "processTransactionRecoveryScaleDown"
	remoteOpsMock := remoteOpsMock{ExecuteMockReturn: [][]string{
		{`{"outcome": "success", "result": ["transactions"]}`, "child-type=subsystem"}, // transaction subsystem available
		{`{"outcome": "success", "result": "stopped"}`, "server-state"},                // app server status is not 'running'
	}}
	wildflyutil.RemoteOps = &remoteOpsMock

	// Reconcile for the scale down - updating the pod state at the wildflyserver CR
	_, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)
	err = cl.Get(context.TODO(), req.NamespacedName, wildflyServer)
	require.NoError(t, err)

	// Expecting the transaction recovery was kicked in and moved the pod under investigation state
	assert.Equal(wildflyv1alpha1.PodStateScalingDownRecoveryInvestigation, wildflyServer.Status.Pods[0].State)
	assert.Equal(int32(1), wildflyServer.Status.ScalingdownPods)
	// StatefulSet is still not scaled-down (under investigation state)
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	assert.Equal(expectedReplicaSize, *statefulSet.Spec.Replicas)
	// expecting the Execute method processed all mock responses
	assert.Empty(remoteOpsMock.ExecuteMockReturn)
}

func TestRecoveryScaleDown(t *testing.T) {
	wildflyServer := defaultWildflyServerDefinition.DeepCopy()
	setupBeforeScaleDown(t, wildflyServer, 1)

	log.Info("WildFly server was reconciled, let's scale it down.", "WildflyServer", wildflyServer)
	wildflyServer.Spec.Replicas = 0
	err := cl.Update(context.TODO(), wildflyServer)

	// Reconcile for the scale down - updating the pod labels
	_, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)

	// Mocking the jboss-cli.sh calls to reach the loop on working with pods at "processTransactionRecoveryScaleDown"
	remoteOpsMock := remoteOpsMock{ExecuteMockReturn: [][]string{
		{`{"outcome": "success", "result": ["transactions"]}`, "child-type=subsystem"}, // list subsystems
		{`{"outcome": "success", "result": "running"}`, "server-state"},                // is wfly running?
		{`{"outcome": "success"}`}, // txn backoff system variable setup
		{``},                       // mkdir org.wildfly.internal.cli.boot.hook.marker.dir
		{`{"outcome": "success"}`}, // system variable org.wildfly.internal.cli.boot.hook.marker.dir
		{`{"outcome": "success", "result" : null}`},                     // shutdown(restart=true)
		{`{"outcome": "success", "result": "running"}`, "server-state"}, // is wfly running after restart?
		{`{"outcome": "success"}`},                                      // recovery listener verification
		{`{"outcome": "success", "result": "4712"}`},                    // query recovery port
		{`{"outcome": "success"}`},                                      // recursive read of socket binding, skipping this
		// #1
		{`{"outcome": "success"}`, "probe"}, // txn log-store :probe after recovery scan"
		{`{"outcome": "success"}`},          // read log-store after :probe
		{``},                                // ls marker dir
		// #2
		{`{"outcome": "success"}`, "probe"}, // txn log-store :probe after recovery scan
		{`{"outcome": "success"}`},          // read log-store after :probe
		{``},                                // ls marker dir
	}}
	wildflyutil.RemoteOps = &remoteOpsMock

	// Reconcile for the scale down - updating the pod state at the wildflyserver CR
	_, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)
	err = cl.Get(context.TODO(), req.NamespacedName, wildflyServer)
	require.NoError(t, err)

	// Txn recovery processing finished with a success
	// StatefulSet needs to be updated in scaled down manner
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	assert.Equal(int32(0), *statefulSet.Spec.Replicas)
	// WildFlyServer status needs to be updated
	err = cl.Get(context.TODO(), req.NamespacedName, wildflyServer)
	require.NoError(t, err)
	assert.Equal(wildflyv1alpha1.PodStateScalingDownClean, wildflyServer.Status.Pods[0].State)

	// Reconcile is waiting for StatefulSet controller to remove pods
	_, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)
	err = cl.Get(context.TODO(), req.NamespacedName, wildflyServer)
	assert.Equal(int32(1), wildflyServer.Status.Replicas)
	// Simulating the StatefulSet controller to change number of pods and its status
	statefulSet.Status.Replicas = 0
	err = cl.Update(context.TODO(), statefulSet)
	require.NoError(t, err)
	podList, err := GetPodsForWildFly(r, wildflyServer)
	err = cl.Delete(context.TODO(), &podList.Items[0])
	require.NoError(t, err)
	// Reconcile makes the WildFlyServer status to be changed
	_, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)
	err = cl.Get(context.TODO(), req.NamespacedName, wildflyServer)
	require.NoError(t, err)
	assert.Equal(int32(0), wildflyServer.Status.Replicas)
	// expecting the Execute method processed all mock responses
	assert.Empty(remoteOpsMock.ExecuteMockReturn)
}

func TestSkipRecoveryScaleDownWhenNoTxnSubsystem(t *testing.T) {
	wildflyServer := defaultWildflyServerDefinition.DeepCopy()
	setupBeforeScaleDown(t, wildflyServer, 1)

	log.Info("WildFly server was reconciled, let's scale it down.", "WildflyServer", wildflyServer)
	wildflyServer.Spec.Replicas = 0
	err := cl.Update(context.TODO(), wildflyServer)

	// Reconcile for the scale down - updating the pod labels
	_, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)

	// Mocking the jboss-cli.sh calls to return of there is no transactions subsystem available
	remoteOpsMock := remoteOpsMock{ExecuteMockReturn: [][]string{
		{`{"outcome": "success", "result": ["nothing"]}`, "child-type=subsystem"}, // does not contain "transactions"
	}}
	wildflyutil.RemoteOps = &remoteOpsMock

	// Reconcile for the txn scale down processing - recovery skipped
	_, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)
	// StatefulSet needs to be updated in scaled down manner
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	assert.Equal(int32(0), *statefulSet.Spec.Replicas)
	// WildFlyServer status needs to be updated in scaled down manner
	err = cl.Get(context.TODO(), req.NamespacedName, wildflyServer)
	require.NoError(t, err)
	assert.Equal(wildflyv1alpha1.PodStateScalingDownClean, wildflyServer.Status.Pods[0].State)
	// expecting the Execute method processed all mock responses
	assert.Empty(remoteOpsMock.ExecuteMockReturn)
}

func TestSkipRecoveryScaleDownWhenEmptyDirStorage(t *testing.T) {
	wildflyServer := defaultWildflyServerDefinition.DeepCopy()
	wildflyServer.Spec.Storage.EmptyDir = &corev1.EmptyDirVolumeSource{} // define emptydir which refuses the claim to be used
	setupBeforeScaleDown(t, wildflyServer, 1)

	log.Info("WildFly server was reconciled, let's scale it down.", "WildflyServer", wildflyServer)
	wildflyServer.Spec.Replicas = 0
	err := cl.Update(context.TODO(), wildflyServer)

	// Reconcile for the scale down - updating the pod labels
	_, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)

	// Mocking the jboss-cli.sh calls to return that JDBC object store is not used
	remoteOpsMock := remoteOpsMock{ExecuteMockReturn: [][]string{
		{`{"outcome": "success", "result": ["transactions"]}`, "child-type=subsystem"}, // does contain "transactions"
		{`{"outcome": "success", "result": false}`, "use-jdbc-store"},
	}}
	wildflyutil.RemoteOps = &remoteOpsMock

	// Reconcile for the txn scale down procesing - recovery skipped
	_, err = r.Reconcile(context.TODO(), req)
	require.NoError(t, err)
	// StatefulSet needs to be updated in scaled down manner
	statefulSet := &appsv1.StatefulSet{}
	err = cl.Get(context.TODO(), req.NamespacedName, statefulSet)
	assert.Equal(int32(0), *statefulSet.Spec.Replicas)
	// WildFlyServer status needs to be updated in sclaed down manner
	err = cl.Get(context.TODO(), req.NamespacedName, wildflyServer)
	require.NoError(t, err)
	assert.Equal(wildflyv1alpha1.PodStateScalingDownClean, wildflyServer.Status.Pods[0].State)
	// expecting the Execute method processed all mock responses
	assert.Empty(remoteOpsMock.ExecuteMockReturn)
}

// --
// -- Mocking the remote calls and Kubernetes API ---
// --

type eventRecorderMock struct {
}

func (rm eventRecorderMock) Event(object runtime.Object, eventtype, reason, message string) {}
func (rm eventRecorderMock) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
}
func (rm eventRecorderMock) PastEventf(object runtime.Object, timestamp metav1.Time, eventtype, reason, messageFmt string, args ...interface{}) {
}
func (rm eventRecorderMock) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
}

type remoteOpsMock struct {
	// this is a "list of arrays", every top-level-item represents a mock information returned at time of Execute method is invoked,
	// once returned then the inner array is removed from the list
	// the first array item represents what is returned to the caller on invoking Execute
	// the second array item is optional and consist a check against the "command", if the command does not contain the value then panic is thrown.
	ExecuteMockReturn [][]string
}

func (rops *remoteOpsMock) Execute(pod *corev1.Pod, command string) (string, error) {
	stringToReturn := ""
	if len(rops.ExecuteMockReturn) > 0 {
		stringToReturn = rops.ExecuteMockReturn[0][0]
		if len(rops.ExecuteMockReturn[0]) > 1 {
			stringToVerify := rops.ExecuteMockReturn[0][1]
			if !strings.Contains(command, stringToVerify) {
				panic(fmt.Sprintf("The command string '%v' does not contain required string '%v'", command, stringToVerify))
			}
		}
		rops.ExecuteMockReturn = rops.ExecuteMockReturn[1:] // dequeuing, removal of the first item
	}
	log.Info("remoteOpsMock.Execute command:'" + command + "',  returns: '" + stringToReturn + "'")
	return stringToReturn, nil
}
func (rops remoteOpsMock) SocketConnect(hostname string, port int32, command string) (string, error) {
	return "", nil
}
func (rops remoteOpsMock) VerifyLogContainsRegexp(pod *corev1.Pod, logFromTime *time.Time, regexpLineCheck *regexp.Regexp) (string, error) {
	return "", nil
}
func (rops remoteOpsMock) ObtainLogLatestTimestamp(pod *corev1.Pod) (*time.Time, error) {
	nowTime := time.Now()
	return &nowTime, nil
}
