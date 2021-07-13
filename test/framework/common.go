package framework

import (
	goctx "context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// WildFlyBasicTest runs basic operator tests
func WildFlyBasicTest(t *testing.T, applicationTag string, bootableJar bool) {
	ctx, f := wildflyTestSetup(t)
	defer ctx.Cleanup()

	if err := wildflyBasicServerScaleTest(t, f, ctx, applicationTag, bootableJar); err != nil {
		t.Fatal(err)
	}
}

func wildflyTestSetup(t *testing.T) (*framework.Context, *framework.Framework) {
	ctx := framework.NewContext(t)
	err := ctx.InitializeClusterResources(&framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		defer ctx.Cleanup()
		t.Fatalf("Failed to initialize cluster resources: %v", err)
	}
	t.Log("Initialized cluster resources")
	namespace, err := ctx.GetOperatorNamespace()
	if err != nil {
		defer ctx.Cleanup()
		t.Fatalf("Failed to get namespace for testing context '%v': %v", ctx, err)
	}
	t.Logf("Testing in namespace %s", namespace)
	// get global framework variables
	f := framework.Global
	return ctx, f
}

func wildflyBasicServerScaleTest(t *testing.T, f *framework.Framework, ctx *framework.Context, applicationTag string, bootableJar bool) error {
	namespace, err := ctx.GetOperatorNamespace()
	if err != nil {
		return fmt.Errorf("could not get namespace: %v", err)
	}

	name := "example-wildfly-" + unixEpoch()
	// create wildflyserver custom resource
	wildflyServer := MakeBasicWildFlyServer(namespace, name, "quay.io/wildfly-quickstarts/wildfly-operator-quickstart:"+applicationTag, 1, bootableJar)
	err = CreateAndWaitUntilReady(f, ctx, t, wildflyServer)
	if err != nil {
		return err
	}

	t.Logf("Application %s is deployed with %d instance\n", name, 1)

	context := goctx.TODO()

	// update the size to 2
	err = f.Client.Get(context, types.NamespacedName{Name: name, Namespace: namespace}, wildflyServer)
	if err != nil {
		return err
	}
	wildflyServer.Spec.Replicas = 2
	err = f.Client.Update(context, wildflyServer)
	if err != nil {
		return err
	}
	t.Logf("Updated application %s size to %d\n", name, wildflyServer.Spec.Replicas)

	// check that the resource have been updated
	return WaitUntilReady(f, t, wildflyServer)
}

// WildFlyClusterTest runs cluster operator tests
func WildFlyClusterTest(t *testing.T, applicationTag string) {
	ctx, f := wildflyTestSetup(t)
	defer ctx.Cleanup()

	if err := wildflyClusterViewTest(t, f, ctx, applicationTag); err != nil {
		t.Fatal(err)
	}
}

func wildflyClusterViewTest(t *testing.T, f *framework.Framework, ctx *framework.Context, applicationTag string) error {
	namespace, err := ctx.GetOperatorNamespace()
	if err != nil {
		return fmt.Errorf("could not get namespace: %v", err)
	}

	name := "clusterbench-" + unixEpoch()

	// create RBAC so that JGroups can view the k8s cluster
	roleBinding := &rbac.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RoleBinding",
			APIVersion: "rbac.authorization.k8s.io",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subjects: []rbac.Subject{{
			Kind: "ServiceAccount",
			Name: "default",
		}},
		RoleRef: rbac.RoleRef{
			Kind:     "ClusterRole",
			Name:     "view",
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	err = f.Client.Create(goctx.TODO(), roleBinding, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}

	// create wildflyserver custom resource
	wildflyServer := MakeBasicWildFlyServer(namespace, name, "quay.io/wildfly-quickstarts/clusterbench-ee7:"+applicationTag, 2, false)

	err = CreateAndWaitUntilReady(f, ctx, t, wildflyServer)
	if err != nil {
		return err
	}

	return WaitUntilClusterIsFormed(f, t, wildflyServer, name+"-0", name+"-1")
}

// WildflyScaleDownTest runs recovery scale down operation
func WildflyScaleDownTest(t *testing.T, applicationTag string) {
	ctx, f := wildflyTestSetup(t)
	defer ctx.Cleanup()

	namespace, err := ctx.GetOperatorNamespace()
	if err != nil {
		t.Fatalf("could not get namespace: %v", err)
	}

	name := "example-wildfly-" + unixEpoch()
	// create wildflyserver custom resource
	wildflyServer := MakeBasicWildFlyServerWithStorage(namespace, name, "quay.io/wildfly-quickstarts/wildfly-operator-quickstart:"+applicationTag, 2, false)
	// waiting for number of pods matches the desired state
	err = CreateAndWaitUntilReady(f, ctx, t, wildflyServer)
	if err != nil {
		t.Fatalf("Failed while waiting for all resources being initialized based on the WildflyServer definition: %v", err)
	}
	// verification that the size of the instances matches what is expected by the test
	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, wildflyServer)
	if err != nil {
		t.Fatalf("Failed to obtain the WildflyServer resource: %v", err)
	}
	if wildflyServer.Spec.Replicas != 2 {
		t.Fatalf("The created %s customer resource should be defined with 2 instances but it's %v: %v", name, wildflyServer.Spec.Replicas, err)
	}
	// waiting for statefulset to scale to two instances
	if err = WaitUntilReady(f, t, wildflyServer); err != nil {
		t.Fatalf("Failed during waiting till %s customer resource is updated and ready: %v", name, err)
	}
	t.Logf("Application %s is deployed with %d instances\n", name, wildflyServer.Spec.Replicas)

	// scaling down by one
	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, wildflyServer)
	if err != nil {
		t.Fatalf("Failed to obtain the WildflyServer resource for scaling it down: %v", err)
	}
	wildflyServer.Spec.Replicas = 1
	err = f.Client.Update(goctx.TODO(), wildflyServer)
	if err != nil {
		t.Fatalf("Failed to update size of %s resource by decreasing the spec size: %v", name, err)
	}
	t.Logf("Updated application customer resource %s size to %d\n", name, wildflyServer.Spec.Replicas)

	// check that the resource has been updated
	if err = WaitUntilReady(f, t, wildflyServer); err != nil {
		t.Fatalf("Failed during waiting till %s customer resource is updated and ready: %v", name, err)
	}

	// verification that deletion works correctly as finalizers should be run at this call
	if DeleteWildflyServer(wildflyServer, f, t); err != nil {
		t.Fatalf("Failed to wait until the WildflyServer resource is deleted: %v", err)
	}
}

func unixEpoch() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

// IsOperatorLocal returns true if the LOCAL_OPERATOR env var is set to true.
func IsOperatorLocal() bool {
	val, ok := os.LookupEnv("LOCAL_OPERATOR")
	if !ok {
		return false
	}
	local, err := strconv.ParseBool(val)
	if err != nil {
		return false
	}
	return local
}
