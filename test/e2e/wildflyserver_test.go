package e2e

import (
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"log"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("WildFly Server tests", func() {

	// Creates the NS to run the tests and deploy the operator if it's not running in local mode
	BeforeEach(func() {
		ctx := context.Background()

		testNs := &corev1.Namespace{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}

		nsHolder := &corev1.Namespace{}
		err := k8sClient.Create(ctx, testNs)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: testNs.Name}, nsHolder)
			if err != nil && !apierrors.IsAlreadyExists(err) {
				log.Printf("Wait until %s namespace is created", testNs.Name)
				return false
			}
			log.Printf("%s namespace created", testNs.Name)
			return true
		}, timeout, interval).Should(BeTrue())

		if os.Getenv("LOCAL_MANAGER") == "0" {
			log.Printf("Creating the service account")
			saHolder := serviceAccount.DeepCopy()
			err = k8sClient.Create(ctx, saHolder)
			Expect(err).ToNot(HaveOccurred())

			log.Printf("Creating the Role")
			for _, role := range roles {
				log.Printf("Creating the Role %s", role.Name)
				roleHolder := role.DeepCopy()
				err = k8sClient.Create(ctx, roleHolder)
				Expect(err).ToNot(HaveOccurred())
			}

			for _, roleBindings := range roleBindings {
				log.Printf("Creating the Role Binding %s", roleBindings.Name)
				rbHolder := roleBindings.DeepCopy()
				err = k8sClient.Create(ctx, rbHolder)
				Expect(err).ToNot(HaveOccurred())
			}

			log.Printf("Creating the operator")
			opHolder := operator.DeepCopy()
			err = k8sClient.Create(ctx, opHolder)
			Expect(err).ToNot(HaveOccurred())
			WaitUntilDeploymentReady(ctx, k8sClient, opHolder)
		}
	})

	// After each test, delete the test ns
	AfterEach(func() {
		ctx := context.Background()

		testNs := &corev1.Namespace{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}

		log.Printf("Deleting %s namespace", namespace)
		err := k8sClient.Delete(ctx, testNs, client.PropagationPolicy(metav1.DeletePropagationForeground))
		Expect(err).ToNot(HaveOccurred())

		nsHolder := &corev1.Namespace{}
		Eventually(func() bool {
			err = k8sClient.Get(ctx, types.NamespacedName{Name: testNs.Name}, nsHolder)
			if err != nil && apierrors.IsNotFound(err) {
				log.Printf("The %s namespace was deleted", testNs.Name)
				return true
			}
			log.Printf("Waiting until %s namespace is deleted", testNs.Name)
			return false
		}, timeout, interval).Should(BeTrue())
	})

	It("WildFlyServer can be created and scaled up", func() {
		applicationImage := "wildfly/wildfly-test-image:0.0"
		name := "example-wildfly"
		ctx := context.Background()

		server := MakeBasicWildFlyServer(namespace, name, applicationImage, 1, false)

		log.Printf("Creating %s resource", server.Name)
		Expect(k8sClient.Create(ctx, server)).Should(Succeed())

		WaitUntilReady(ctx, k8sClient, server)

		log.Printf("Scalling the server to 2 replicas")
		serverCpy := server.DeepCopy()
		serverCpy.Spec.Replicas = 2
		Expect(k8sClient.Patch(ctx, serverCpy, client.MergeFrom(server))).Should(Succeed())
		WaitUntilReady(ctx, k8sClient, serverCpy)
		WaitUntilServerDeleted(ctx, k8sClient, serverCpy)
	})

	It("WildFlyServer packaged as a Bootable JAR can be created and scaled up", func() {
		applicationImage := "wildfly/bootable-jar-test-image:0.0"
		name := "example-wildfly-bootable-jar"
		ctx := context.Background()

		server := MakeBasicWildFlyServer(namespace, name, applicationImage, 1, true)

		log.Printf("Creating %s resource", server.Name)
		Expect(k8sClient.Create(ctx, server)).Should(Succeed())
		WaitUntilReady(ctx, k8sClient, server)

		log.Printf("Scalling the server to 2 replicas")
		serverCpy := server.DeepCopy()
		serverCpy.Spec.Replicas = 2
		Expect(k8sClient.Patch(ctx, serverCpy, client.MergeFrom(server))).Should(Succeed())
		WaitUntilReady(ctx, k8sClient, serverCpy)
		WaitUntilServerDeleted(ctx, k8sClient, serverCpy)
	})

	It("WildFlyServer can form a cluster", func() {
		applicationImage := "wildfly/clusterbench-test-image:0.0"
		name := "cluster-bench"
		ctx := context.Background()

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

		Expect(k8sClient.Create(ctx, roleBinding)).Should(Succeed())

		server := MakeBasicWildFlyServer(namespace, name, applicationImage, 2, false)
		Expect(k8sClient.Create(ctx, server)).Should(Succeed())
		WaitUntilReady(ctx, k8sClient, server)

		WaitUntilClusterIsFormed(server, name+"-0", name+"-1")
		WaitUntilServerDeleted(ctx, k8sClient, server)
	})

	It("WildFlyServer with an storage can scale down", func() {
		if os.Getenv("LOCAL_MANAGER") != "0" {
			Skip("Skipping this test. It cannot be tested with local manager.")
		}

		applicationImage := "wildfly/wildfly-test-image:0.0"
		name := "wildfly-server-scale-down"
		ctx := context.Background()

		server := MakeBasicWildFlyServerWithStorage(namespace, name, applicationImage, 2, false)

		log.Printf("Creating %s resource", server.Name)
		Expect(k8sClient.Create(ctx, server)).Should(Succeed())
		WaitUntilReady(ctx, k8sClient, server)

		log.Printf("Scalling the server down to 1 replica")
		serverCpy := server.DeepCopy()
		serverCpy.Spec.Replicas = 1
		Expect(k8sClient.Patch(ctx, serverCpy, client.MergeFrom(server))).Should(Succeed())
		WaitUntilReady(ctx, k8sClient, serverCpy)

		// Ensure we have just one replica over a duration of time
		log.Printf("Transaction recovery finished and the server has been scalled down. Waiting %s seconds to ensure the stability.", duration)
		stsHolder := &appsv1.StatefulSet{}
		Consistently(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: serverCpy.Name, Namespace: serverCpy.Namespace}, stsHolder)
			if err != nil {
				log.Printf("There was an error getting the %s StatefulSet: %s", serverCpy.Name, err.Error())
				return false
			}
			log.Printf("StatefulSet expected replicas (%d) Ready (%d/%d)\n", 1, stsHolder.Status.Replicas, stsHolder.Status.ReadyReplicas)

			return stsHolder.Status.Replicas == 1 && stsHolder.Status.ReadyReplicas == 1
		}, duration, interval).Should(BeTrue())

		WaitUntilServerDeleted(ctx, k8sClient, serverCpy)
	})

	It("WildFlyServer Data directory from JBOSS_HOME environment value contains kernel directory", func() {
		applicationImage := "wildfly/wildfly-test-image:0.0"
		name := "example-wildfly"
		ctx := context.Background()

		server := MakeBasicWildFlyServer(namespace, name, applicationImage, 1, false)

		log.Printf("Creating %s resource", server.Name)
		Expect(k8sClient.Create(ctx, server)).Should(Succeed())

		WaitUntilReady(ctx, k8sClient, server)

		statefulSet, err := GetExistingStatefulSet(ctx, k8sClient, server)
		if err != nil {
			log.Printf("Failed to get the StatefulSet to verify the server home directory")
			log.Print(err)
			Fail("Failed to get the StatefulSet to verify the server home directory")
		}

		pods, err := ListPodsByStatefulSet(ctx, k8sClient, statefulSet)
		if err != nil {
			log.Printf("Failed to get the Pods to verify the server home directory")
			log.Print(err)
			Fail("Failed to get the Pods to verify the server home directory")
		}

		// Verify the server home directory exists
		jbossHome := os.Getenv("JBOSS_HOME") + "/standalone/data/kernel"
		for _, pod := range pods {
			exists, err := CheckDirectoryExists(&pod, jbossHome)
			if err != nil || !exists {
				log.Printf("Failed to verify the server home directory for the pod: %s. exists: %t", pod.Name, exists)
				log.Print(err)
				Fail("Failed to verify the server home directory for the pod: " + pod.Name)
			}
		}

		WaitUntilReady(ctx, k8sClient, server)
		WaitUntilServerDeleted(ctx, k8sClient, server)
	})
})
