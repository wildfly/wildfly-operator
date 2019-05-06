package framework

import (
	"bytes"
	goctx "context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

var (
	retryInterval        = time.Second * 5
	timeout              = time.Minute * 3
	cleanupRetryInterval = time.Second * 1
	cleanupTimeout       = time.Second * 5
)

// MakeBasicWildFlyServer creates a basic WildFlyServer resource
func MakeBasicWildFlyServer(ns, name, applicationImage string, size int32) *wildflyv1alpha1.WildFlyServer {
	return &wildflyv1alpha1.WildFlyServer{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WildFlyServer",
			APIVersion: "wildfly.org/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   ns,
			Annotations: map[string]string{},
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			ApplicationImage: applicationImage,
			Size:             size,
		},
	}
}

// CreateStandaloneConfigMap creates a ConfigMap for the standalone configuration
func CreateStandaloneConfigMap(f *framework.Framework, ctx *framework.TestCtx, ns string, name string, key string, file []byte) error {
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
func CreateAndWaitUntilReady(f *framework.Framework, ctx *framework.TestCtx, t *testing.T, server *wildflyv1alpha1.WildFlyServer) error {
	// use TestCtx's create helper to create the object and add a cleanup function for the new object
	err := f.Client.Create(goctx.TODO(), server, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}

	err = WaitUntilReady(f, t, server)
	if err != nil {
		return err
	}

	return nil
}

// WaitUntilReady waits until the stateful set replicas matches the server spec size.
func WaitUntilReady(f *framework.Framework, t *testing.T, server *wildflyv1alpha1.WildFlyServer) error {
	name := server.ObjectMeta.Name
	ns := server.ObjectMeta.Namespace
	size := server.Spec.Size

	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {

		statefulSet, err := f.KubeClient.AppsV1().StatefulSets(ns).Get(name, metav1.GetOptions{IncludeUninitialized: true})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		if statefulSet.Status.Replicas == size {
			return true, nil
		}

		t.Logf("Waiting for full availability of %s statefulset (%d/%d)\n", name, statefulSet.Status.Replicas, size)
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

	pattern := fmt.Sprintf(".*ISPN000094: Received new cluster view.*(%s, %s|%[2]s, %[1]s).*", podName1, podName2)

	err := wait.Poll(30*time.Second, 5*time.Minute, func() (done bool, err error) {
		var clusterFormed bool

		for _, podName := range []string{podName1, podName2} {
			logs, err := GetLogs(f, server, podName)
			if err != nil {
				return false, err
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
