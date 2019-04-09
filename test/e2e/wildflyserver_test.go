// +build !unit

package e2e

import (
	goctx "context"
	"fmt"
	"testing"
	"time"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	"github.com/wildfly/wildfly-operator/pkg/apis"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

var (
	retryInterval        = time.Second * 5
	timeout              = time.Second * 60
	cleanupRetryInterval = time.Second * 1
	cleanupTimeout       = time.Second * 5
)

func TestWildFlyServer(t *testing.T) {
	wildflyServerList := &wildflyv1alpha1.WildFlyServerList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WildFlyServer",
			APIVersion: "wildfly.org/v1alpha1",
		},
	}
	err := framework.AddToFrameworkScheme(apis.AddToScheme, wildflyServerList)
	if err != nil {
		t.Fatalf("failed to add custom resource scheme to framework: %v", err)
	}
	// run subtests
	t.Run("wildlfyserver-group", func(t *testing.T) {
		t.Run("Cluster", WildFlyCluster)
		t.Run("Cluster2", WildFlyCluster)
	})
}

func wildflyServerScaleTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) error {
	namespace, err := ctx.GetNamespace()
	if err != nil {
		return fmt.Errorf("could not get namespace: %v", err)
	}
	// create wildflyserver custom resource
	exampleWildFlyServer := &wildflyv1alpha1.WildFlyServer{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WildFlyServer",
			APIVersion: "wildfly.org/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-wildfly",
			Namespace: namespace,
		},
		Spec: wildflyv1alpha1.WildFlyServerSpec{
			Size:             2,
			ApplicationImage: "quay.io/jmesnil/wildfly-operator-quickstart",
		},
	}
	// use TestCtx's create helper to create the object and add a cleanup function for the new object
	err = f.Client.Create(goctx.TODO(), exampleWildFlyServer, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}
	// wait for example-wildfly to reach 2 replicas
	err = waitForStatefulSet(t, f.KubeClient, namespace, "example-wildfly", 2, retryInterval, timeout)
	if err != nil {
		return err
	}

	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: "example-wildfly", Namespace: namespace}, exampleWildFlyServer)
	if err != nil {
		return err
	}
	exampleWildFlyServer.Spec.Size = 3
	err = f.Client.Update(goctx.TODO(), exampleWildFlyServer)
	if err != nil {
		return err
	}

	// wait for example-memcwildflyached to reach 3 replicas
	return waitForStatefulSet(t, f.KubeClient, namespace, "example-wildfly", 3, retryInterval, timeout)
}

func WildFlyCluster(t *testing.T) {
	t.Parallel()
	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()
	err := ctx.InitializeClusterResources(&framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		t.Fatalf("failed to initialize cluster resources: %v", err)
	}
	t.Log("Initialized cluster resources")
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	// get global framework variables
	f := framework.Global
	// wait for memcached-operator to be ready
	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "wildfly-operator", 1, retryInterval, timeout)
	if err != nil {
		t.Fatal(err)
	}

	if err = wildflyServerScaleTest(t, f, ctx); err != nil {
		t.Fatal(err)
	}
}

func waitForStatefulSet(t *testing.T, kubeclient kubernetes.Interface, namespace, name string, replicas int, retryInterval, timeout time.Duration) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		statefulSet, err := kubeclient.AppsV1().StatefulSets(namespace).Get(name, metav1.GetOptions{IncludeUninitialized: true})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of %s deployment\n", name)
				return false, nil
			}
			return false, err
		}

		if int(statefulSet.Status.ReadyReplicas) == replicas {
			return true, nil
		}
		t.Logf("Waiting for full availability of %s statefulset (%d/%d)\n", name, statefulSet.Status.ReadyReplicas, replicas)
		return false, nil
	})
	if err != nil {
		return err
	}
	t.Logf("statefulset available (%d/%d)\n", replicas, replicas)
	return nil
}
