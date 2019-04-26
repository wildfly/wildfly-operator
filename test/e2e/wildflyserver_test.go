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
	wildflyframework "github.com/wildfly/wildfly-operator/test/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	retryInterval        = time.Second * 5
	timeout              = time.Minute * 3
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
	t.Run("wildflyserver-group", func(t *testing.T) {
		t.Run("Cluster", WildFlyCluster)
	})
}

func wildflyServerScaleTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) error {
	namespace, err := ctx.GetNamespace()
	if err != nil {
		return fmt.Errorf("could not get namespace: %v", err)
	}

	name := "example-wildfly"
	// create wildflyserver custom resource
	wildflyServer := wildflyframework.MakeBasicWildFlyServer(namespace, name, "quay.io/jmesnil/wildfly-operator-quickstart", 2)
	err = wildflyframework.CreateAndWaitUntilReady(f, ctx, t, wildflyServer)
	if err != nil {
		return err
	}

	// update the size to 3
	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, wildflyServer)
	if err != nil {
		return err
	}
	wildflyServer.Spec.Size = 3
	err = f.Client.Update(goctx.TODO(), wildflyServer)
	if err != nil {
		return err
	}

	// check that the resource have been updated
	return wildflyframework.WaitUntilReady(f, t, wildflyServer)
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
	// wait for wildfly-operator to be ready
	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "wildfly-operator", 1, retryInterval, timeout)
	if err != nil {
		t.Fatal(err)
	}

	if err = wildflyServerScaleTest(t, f, ctx); err != nil {
		t.Fatal(err)
	}
}
