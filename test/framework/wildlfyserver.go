package framework

import (
	"bytes"
	"context"
	goctx "context"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/api/resource"
	"regexp"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

var (
	retryInterval        = time.Second * 5
	timeout              = time.Minute * 5
	cleanupRetryInterval = time.Second * 1
	cleanupTimeout       = time.Second * 5
)

// MakeBasicWildFlyServer creates a basic WildFlyServer resource
func MakeBasicWildFlyServer(ns, name, applicationImage string, size int32, bootableJar bool) *wildflyv1alpha1.WildFlyServer {
	return &wildflyv1alpha1.WildFlyServer{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WildFlyServer",
			APIVersion: "wildfly.org/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Replicas:         size,
			BootableJar: bootableJar,
		},
	}
}

// MakeBasicWildFlyServerWithStorage creates a basic WildFlyServer resource configured with a persistent storage assuming it will be provisioned
// dynamically by the cluster provider
func MakeBasicWildFlyServerWithStorage(ns, name, applicationImage string, size int32, bootableJar bool) *wildflyv1alpha1.WildFlyServer {
	return &wildflyv1alpha1.WildFlyServer{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WildFlyServer",
			APIVersion: "wildfly.org/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Replicas:         size,
			BootableJar:      bootableJar,
			Storage: &wildflyv1alpha1.StorageSpec{
				VolumeClaimTemplate: corev1.PersistentVolumeClaim{
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("5Mi"),
							},
						},
					},
				},
			},
		},
	}
}

// CreateStandaloneConfigMap creates a ConfigMap for the standalone configuration
func CreateStandaloneConfigMap(f *framework.Framework, ctx *framework.Context, ns string, name string, key string, file []byte) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   ns,
			Name:        name,
			Annotations: map[string]string{},
		},
		BinaryData: map[string][]byte{
			key: file,
		},
	}
	return f.Client.Create(goctx.TODO(), configMap, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
}

// CreateAndWaitUntilReady creates a WildFlyServer resource and wait until it is ready
func CreateAndWaitUntilReady(f *framework.Framework, ctx *framework.Context, t *testing.T, server *wildflyv1alpha1.WildFlyServer) error {
	// use Context's create helper to create the object and add a cleanup function for the new object
	err := f.Client.Create(goctx.TODO(), server, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}

	// removing finalizers explicitly otherwise the removal could hang
	ctx.AddCleanupFn(
		func() error {
			// Removing deployment for not putting finalizers back to the WildflyServer
			name := server.ObjectMeta.Name
			namespace := server.ObjectMeta.Namespace
			deployment, err := f.KubeClient.AppsV1().Deployments(namespace).Get("wildfly-operator", metav1.GetOptions{})
			if err == nil && deployment != nil {
				t.Logf("Cleaning deployment '%v'\n", deployment.Name)
				f.Client.Delete(goctx.TODO(), deployment)
			}
			// Cleaning finalizer
			wait.Poll(retryInterval, timeout, func() (done bool, err error) {
				foundWildflyServer := &wildflyv1alpha1.WildFlyServer{}
				namespacedName := types.NamespacedName{Name: name, Namespace: namespace}
				if errPoll := f.Client.Get(context.TODO(), namespacedName, foundWildflyServer); errPoll != nil {
					if apierrors.IsNotFound(errPoll) {
						t.Logf("No WildFlyServer object '%v' to remove the finalizer at. Probably all cleanly finished before.\n", name)
						return true, nil
					}
					t.Logf("Cannot obtain object of the WildflyServer '%v', cause: %v\n", name, errPoll)
					return false, nil
				}
				foundWildflyServer.SetFinalizers([]string{})
				if errPoll := f.Client.Update(context.TODO(), foundWildflyServer); errPoll != nil {
					t.Logf("Cannot update WildflyServer '%v' with empty finalizers array, cause: %v\n", name, errPoll)
					return false, nil
				}
				t.Logf("Finalizer definition succesfully removed from the WildflyServer '%v'\n", name)
				return true, nil
			})

			namespaceResource, err := f.KubeClient.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
			if err == nil && namespaceResource != nil {
				t.Logf("Cleaning namespace '%v'\n", namespace)
				f.Client.Delete(goctx.TODO(), namespaceResource)
			}

			return err
		},
	)

	return WaitUntilReady(f, t, server)
}

// WaitUntilReady waits until the stateful set replicas matches the server spec size.
func WaitUntilReady(f *framework.Framework, t *testing.T, server *wildflyv1alpha1.WildFlyServer) error {
	name := server.ObjectMeta.Name
	ns := server.ObjectMeta.Namespace
	size := server.Spec.Replicas

	t.Logf("Waiting until statefulset %s is ready with size of %v", name, size)

	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {

		statefulSet, err := f.KubeClient.AppsV1().StatefulSets(ns).Get(name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Statefulset %s not found", name)

				return false, nil
			}
			t.Logf("Got error when getting statefulset %s: %s", name, err)
			return false, err
		}

		if statefulSet.Status.Replicas == size && statefulSet.Status.ReadyReplicas == size {
			return true, nil
		}

		t.Logf("Waiting for full availability of %s statefulset. Requested Replicas (%d) Ready (%d/%d)\n", name, size, statefulSet.Status.ReadyReplicas, statefulSet.Status.Replicas)
		return false, nil
	})
	if err != nil {
		return err
	}
	t.Logf("statefulset available (%d/%d)\n", size, size)

	return nil
}

// WaitUntilWildFlyServerIstarted waits until the WildFly server in the Pod is started.
func WaitUntilWildFlyServerIstarted(f *framework.Framework, t *testing.T, server *wildflyv1alpha1.WildFlyServer, podName string) error {

	err := wait.Poll(30*time.Second, 5*time.Minute, func() (done bool, err error) {
		logs, err := GetLogs(f, server, podName)
		if err != nil {
			return false, err
		}

		// check logs for WFLYSRV0025 (server is started)
		if strings.Contains(logs, "WFLYSRV0025") {
			return true, nil
		}
		t.Logf("Waiting for WildFly in %s to be be started", podName)
		t.Logf(logs)
		return false, nil
	})
	if err != nil {
		return err
	}
	t.Logf("WildFly server started in %s", podName)
	return nil
}

// WaitUntilClusterIsFormed wait until a cluster is formed with all the podNames
func WaitUntilClusterIsFormed(f *framework.Framework, t *testing.T, server *wildflyv1alpha1.WildFlyServer, podName1 string, podName2 string) error {

	pattern := fmt.Sprintf(".*ISPN000094: Received new cluster view.*(.*%s, .*%s|.*%[2]s, .*%[1]s).*", podName1[len(podName1)-23:], podName2[len(podName2)-23:])

	err := wait.Poll(30*time.Second, 5*time.Minute, func() (done bool, err error) {
		var clusterFormed bool

		for _, podName := range []string{podName1, podName2} {
			logs, err := GetLogs(f, server, podName)
			if err != nil {
				t.Logf("[%v] Can't get log for %s. Probably waiting for the container being started "+
					"(e.g. pod could be still in state 'ContainerCreating'), error: %v", time.Now().String(), podName, err)
				return false, nil
			}

			match, _ := regexp.MatchString(pattern, logs)

			if match {
				clusterFormed = true
				t.Logf("got cluster view log in %s", podName)
			} else {
				clusterFormed = false
				t.Logf("Waiting for cluster view log in %s", podName)
				t.Logf(logs)
			}
		}
		return clusterFormed, nil
	})
	if err != nil {
		return err
	}
	t.Logf("Cluster view formed with %s & %s", podName1, podName2)
	return nil
}

// GetLogs returns the logs from the given pod (in the server's namespace).
func GetLogs(f *framework.Framework, server *wildflyv1alpha1.WildFlyServer, podName string) (string, error) {
	logsReq := f.KubeClient.CoreV1().Pods(server.ObjectMeta.Namespace).GetLogs(podName, &corev1.PodLogOptions{})
	podLogs, err := logsReq.Stream()
	if err != nil {
		return "", err
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", err
	}
	logs := buf.String()
	return logs, nil
}

// GetWildflyServer returns the WildflyServer took from Kubernetes API
func GetWildflyServer(name string, namespace string, f *framework.Framework) (*wildflyv1alpha1.WildFlyServer, error) {
	// Fetch the WildFlyServer instance
	wildflyServer := &wildflyv1alpha1.WildFlyServer{}
	namespacedName := types.NamespacedName{Name: name, Namespace: namespace}
	err := f.Client.Get(context.TODO(), namespacedName, wildflyServer)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return wildflyServer, nil
}

// DeleteWildflyServer deletes the instance of WildflyServer and waits for the underlaying StatefulSet is removed altogether
func DeleteWildflyServer(context goctx.Context, wildflyServer *wildflyv1alpha1.WildFlyServer, f *framework.Framework, t *testing.T) error {
	err := f.Client.Delete(context, wildflyServer)
	if err != nil {
		t.Fatalf("Failed to delete of WildflyServer resource: %v", err)
	}
	name := wildflyServer.ObjectMeta.Name
	namespace := wildflyServer.ObjectMeta.Namespace
	t.Logf("WildflyServer resource of application %s was deleted\n", name)
	err = wait.Poll(retryInterval, timeout, func() (bool, error) {
		_, err := f.KubeClient.AppsV1().StatefulSets(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Statefulset %s was not found. It was probably successfully deleted already.", name)
				return true, nil
			}
			t.Logf("Got error when getting statefulset %s: %s", name, err)
			return false, err
		}
		t.Logf("Waiting for statefulset being deleted...")
		return false, nil
	})
	return nil
}
