// +build !unit

package e2e

import (
	"testing"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/wildfly/wildfly-operator/pkg/apis"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	wildflyframework "github.com/wildfly/wildfly-operator/test/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	t.Run("Basic Test", wildFlyBasicTest)
	t.Run("Basic Test for Bootable Jar", wildFlyBootableBasicTest)
	t.Run("Cluster Test", wildFlyClusterTest)

	if !wildflyframework.IsOperatorLocal() {
		// This test is is disabled with a local operator
		// as it requires a network connection from the
		//operator to the application pod.
		//
		// It can be run when the operator is inside the container platform.
		// However for the CI tests, that means that it will not use the operator code
		// from the same commit but the latest image from wildfly/wildfly-operator
		// (corresponding to the latest commit on master branch)
		t.Run("ScaleDownTest", wildflyScaleDownTest)
	}
}

func wildFlyBasicTest(t *testing.T) {
	wildflyframework.WildFlyBasicTest(t, "18.0", false)
}

func wildFlyBootableBasicTest(t *testing.T) {
	wildflyframework.WildFlyBasicTest(t, "bootable-21.0", true)
}

func wildFlyClusterTest(t *testing.T) {
	wildflyframework.WildFlyClusterTest(t, "22.0")
}

func wildflyScaleDownTest(t *testing.T) {
	wildflyframework.WildflyScaleDownTest(t, "18.0")
}
