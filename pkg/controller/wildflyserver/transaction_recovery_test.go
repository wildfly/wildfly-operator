package wildflyserver

import (
	"context"
	"testing"
	"time"

	"github.com/operator-framework/operator-sdk/pkg/log/zap"

	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	wildflyutil "github.com/wildfly/wildfly-operator/pkg/controller/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestWildFlyServerControllerScaleDown(t *testing.T) {
	// Set the logger to development mode for verbose logs.
	logf.SetLogger(zap.Logger())
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
			Replicas:         expectedReplicaSize,
			SessionAffinity:  sessionAffinity,
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
	r := &ReconcileWildFlyServer{client: cl, scheme: s, recorder: eventRecorderMock{}, isOpenShift: false}
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
	wildflyServer.Spec.Replicas = 0
	err = cl.Update(context.TODO(), wildflyServer)

	// Reconcile for the scale down - updating the pod labels
	_, err = r.Reconcile(req)
	require.NoError(t, err)
	// Pod label has to be changed to not being active for service
	podList, err := GetPodsForWildFly(r, wildflyServer)
	require.NoError(t, err)
	assert.Equal(int(expectedReplicaSize), len(podList.Items))
	assert.Equal("disabled", podList.Items[0].GetLabels()["wildfly.org/operated-by-loadbalancer"])

	// mocking the jboss-cli.sh calls to reach the loop on working with pods at "processTransactionRecoveryScaleDown"
	wildflyutil.RemoteOps = &remoteOpsMock{executeReturnString: []string{
		`{"outcome": "success", "result": ["transactions"]}`,
		`{"outcome": "success", "result": false}`,
	}}

	// Reconcile for the scale down - updating the pod state at the wildflyserver CR
	_, err = r.Reconcile(req) // error could be returned here as the scaledown was not sucessful here
	err = cl.Get(context.TODO(), req.NamespacedName, wildflyServer)
	require.NoError(t, err)

	assert.Equal(wildflyv1alpha1.PodStateScalingDownRecoveryInvestigation, wildflyServer.Status.Pods[0].State)
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
	executeReturnString []string
}

func (rops remoteOpsMock) Execute(pod *corev1.Pod, command string) (string, error) {
	stringToReturn := ""
	if len(rops.executeReturnString) > 0 {
		stringToReturn = rops.executeReturnString[0]
		rops.executeReturnString = rops.executeReturnString[1:] // dequeuing, removal of the first item
	}
	return stringToReturn, nil
}
func (rops remoteOpsMock) SocketConnect(hostname string, port int32, command string) (string, error) {
	return "", nil
}
