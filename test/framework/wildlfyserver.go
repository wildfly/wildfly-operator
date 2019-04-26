package framework

import (
	goctx "context"
	"testing"
	"time"

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
