package wildflyserver

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	wildflyutil "github.com/wildfly/wildfly-operator/pkg/controller/util"
	"github.com/wildfly/wildfly-operator/pkg/resources"
	"github.com/wildfly/wildfly-operator/pkg/resources/routes"
	"github.com/wildfly/wildfly-operator/pkg/resources/services"

	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	openshiftutils "github.com/RHsyseng/operator-utils/pkg/utils/openshift"
)

const (
	controllerName                      = "wildflyserver-controller"
	httpApplicationPort           int32 = 8080
	httpManagementPort            int32 = 9990
	defaultRecoveryPort           int32 = 4712
	wildflyServerFinalizer              = "finalizer.wildfly.org"
	wftcDataDirName                     = "ejb-xa-recovery"                      // data directory where WFTC stores transaction runtime data
	markerOperatedByLoadbalancer        = "wildfly.org/operated-by-loadbalancer" // label used to remove a pod from receiving load from loadbalancer during transaction recovery
	markerOperatedByHeadless            = "wildfly.org/operated-by-headless"     // label used to remove a pod from receiving load from headless service when it's cleaned to shutdown
	markerServiceDisabled               = "disabled"                             // label value for the pod that's clean from scaledown and it should be removed from service
	markerServiceActive                 = "active"                               // marking a pod as actively served by its service
	markerRecoveryPort                  = "recovery-port"                        // annotation name to save recovery port
	markerRecoveryPropertiesSetup       = "recovery-properties-setup"            // annotation name to declare that recovery properties were setup for app server
	txnRecoveryScanCommand              = "SCAN"                                 // Narayana socket command to force recovery
)

var (
	log                         = logf.Log.WithName("controller_wildflyserver")
	jbossHome                   = os.Getenv("JBOSS_HOME")
	wildflyFinalizerEnv         = os.Getenv("ENABLE_WILDFLY_FINALIZER")
	standaloneServerDataDirPath = jbossHome + "/standalone/data"
	recoveryErrorRegExp         = regexp.MustCompile("ERROR.*Periodic Recovery")
)

// Add creates a new WildFlyServer Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileWildFlyServer{
		client:             mgr.GetClient(),
		scheme:             mgr.GetScheme(),
		isOpenShift:        isOpenShift(mgr.GetConfig()),
		isWildFlyFinalizer: isWildFlyFinalizer(),
		recorder:           mgr.GetRecorder(controllerName),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(controllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource WildFlyServer
	err = c.Watch(&source.Kind{Type: &wildflyv1alpha1.WildFlyServer{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resources and requeue the owner WildFlyServer
	enqueueRequestForOwner := handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &wildflyv1alpha1.WildFlyServer{},
	}
	for _, obj := range []runtime.Object{&appsv1.StatefulSet{}, &corev1.Service{}} {
		if err = c.Watch(&source.Kind{Type: obj}, &enqueueRequestForOwner); err != nil {
			return err
		}
	}

	// watch for Route only on OpenShift
	if isOpenShift(mgr.GetConfig()) {
		if err = c.Watch(&source.Kind{Type: &routev1.Route{}}, &enqueueRequestForOwner); err != nil {
			return err
		}
	}
	return nil
}

var _ reconcile.Reconciler = &ReconcileWildFlyServer{}

// ReconcileWildFlyServer reconciles a WildFlyServer object
type ReconcileWildFlyServer struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client   client.Client
	scheme   *runtime.Scheme
	recorder record.EventRecorder
	// returns true if the operator is running on OpenShift
	isOpenShift bool
	// returns true if wilfly finalizer should be enabled
	isWildFlyFinalizer bool
}

// Reconcile reads that state of the cluster for a WildFlyServer object and makes changes based on the state read
// and what is in the WildFlyServer.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileWildFlyServer) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling WildFlyServer")

	// Fetch the WildFlyServer instance
	wildflyServer := &wildflyv1alpha1.WildFlyServer{}
	err := r.client.Get(context.TODO(), request.NamespacedName, wildflyServer)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Check if the statefulSet already exists, if not create a new one
	foundStatefulSet := &appsv1.StatefulSet{}
	err = resources.Get(wildflyServer, types.NamespacedName{Name: wildflyServer.Name, Namespace: wildflyServer.Namespace}, r.client, foundStatefulSet)
	if err != nil && errors.IsNotFound(err) {
		// Define a new statefulSet
		statefulSet := r.statefulSetForWildFly(wildflyServer)
		if err = resources.Create(wildflyServer, r.client, r.scheme, statefulSet); err != nil {
			return reconcile.Result{}, err
		}
		// StatefulSet created successfully - return and requeue
		return reconcile.Result{}, nil
	} else if err != nil {
		reqLogger.Error(err, "Failed to get StatefulSet.")
		return reconcile.Result{}, err
	}

	// Check if the WildFlyServer instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isWildflyServerMarkedToBeDeleted := wildflyServer.GetDeletionTimestamp() != nil
	if isWildflyServerMarkedToBeDeleted {
		reqLogger.Info("WildflyServer is marked for deletion. Waiting for finalizers to clean the workspace")
		if wildflyutil.ContainsInList(wildflyServer.GetFinalizers(), wildflyServerFinalizer) {
			// Run finalization logic for WildflyServer. If fails do not remove.
			//   this will be retried at next reconciliation.
			r.recorder.Event(wildflyServer, corev1.EventTypeNormal, "WildFlyServerDeletion",
				"Scaledown processing started before removal. Consult operator log for status.")
			if err := r.finalizeWildflyServer(reqLogger, wildflyServer); err != nil {
				return reconcile.Result{}, err
			}

			// Remove WildflyServer. Once all finalizers have been removed, the object will be deleted.
			wildflyServer.SetFinalizers(wildflyutil.RemoveFromList(wildflyServer.GetFinalizers(), wildflyServerFinalizer))
			if err := resources.Update(wildflyServer, r.client, wildflyServer); err != nil {
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}
	// Add finalizer for this CR
	if r.isWildFlyFinalizer && !wildflyutil.ContainsInList(wildflyServer.GetFinalizers(), wildflyServerFinalizer) {
		if err := r.addWildflyServerFinalizer(reqLogger, wildflyServer); err != nil {
			return reconcile.Result{}, err
		}
	}
	if !r.isWildFlyFinalizer && wildflyutil.ContainsInList(wildflyServer.GetFinalizers(), wildflyServerFinalizer) {
		reqLogger.Info("WildFly finalizer is marked to not be used but it exists at the spec, removing it")
		wildflyServer.SetFinalizers(wildflyutil.RemoveFromList(wildflyServer.GetFinalizers(), wildflyServerFinalizer))
		if err := resources.Update(wildflyServer, r.client, wildflyServer); err != nil {
			return reconcile.Result{}, err
		}
		// Finalizer removed succesfully - return and requeu
		return reconcile.Result{Requeue: true}, nil
	}

	// List of pods which belongs under this WildflyServer instance
	podList, err := r.getPodsForWildFly(wildflyServer)
	if err != nil {
		reqLogger.Error(err, "Failed to list pods.", "WildFlyServer.Namespace", wildflyServer.Namespace, "WildFlyServer.Name", wildflyServer.Name)
		return reconcile.Result{}, err
	}
	wildflyServerSpecSize := wildflyServer.Spec.Size
	statefulsetSpecSize := *foundStatefulSet.Spec.Replicas
	numberOfDeployedPods := int32(len(podList.Items))

	if statefulsetSpecSize == wildflyServerSpecSize && numberOfDeployedPods != wildflyServerSpecSize {
		reqLogger.Info("Number of pods does not match the WildFlyServer specification. Waiting to get numbers in sync.",
			"WildflyServer specification", wildflyServer.Name, "Expected number of pods", wildflyServerSpecSize, "Number of deployed pods", numberOfDeployedPods,
			"StatefulSet spec size", statefulsetSpecSize)
		return reconcile.Result{}, nil
	}

	// Processing scaled down
	numberOfPodsToScaleDown := statefulsetSpecSize - wildflyServerSpecSize // difference between desired pod count and the current number of pods
	// Update pods which are to be scaled down to not be getting requests through loadbalancer
	updated, err := r.setLabelAsDisabled(wildflyServer, reqLogger, markerOperatedByLoadbalancer, int(numberOfPodsToScaleDown), podList, nil, "")
	if updated || err != nil { // labels were updated (updated == true) or some error occured (err != nil)
		return reconcile.Result{Requeue: updated}, err
	}
	// Processing recovery on pods which are planned to be removed because of scale down is in progress now
	mustReconcile, err := r.processTransactionRecoveryScaleDown(reqLogger, wildflyServer, int(numberOfPodsToScaleDown), podList)
	if err != nil {
		reqLogger.Error(err, "Failures during scaling down recovery processing", "Desired replica size", wildflyServerSpecSize,
			"Number of pods to be removed", numberOfPodsToScaleDown)
	}
	if mustReconcile { // server state was updated, we need to reconcile
		return reconcile.Result{Requeue: true}, err
	}

	mustReconcile, requeue, err := r.checkStatefulSet(wildflyServer, foundStatefulSet, podList)
	if mustReconcile {
		return reconcile.Result{Requeue: requeue}, err
	} else if err != nil {
		return reconcile.Result{}, err
	}

	// Check if the loadbalancer already exists, if not create a new one
	loadBalancer, err := services.GetOrCreateNewLoadBalancerService(wildflyServer, r.client, r.scheme, labelsForWildFly(wildflyServer))
	if err != nil {
		return reconcile.Result{}, err
	} else if loadBalancer == nil {
		return reconcile.Result{Requeue: true}, nil
	}
	mustReconcile, requeue, err = r.checkLoadBalancer(wildflyServer, loadBalancer)
	if mustReconcile {
		return reconcile.Result{Requeue: requeue}, err
	} else if err != nil {
		return reconcile.Result{}, err
	}

	// Check if the headless service already exists, if not create a new one
	if headlessService, err := services.GetOrCreateNewHeadlessService(wildflyServer, r.client, r.scheme, labelsForWildFly(wildflyServer)); err != nil {
		return reconcile.Result{}, err
	} else if headlessService == nil {
		return reconcile.Result{}, nil
	}

	// Check if the HTTP route must be created.
	var route *routev1.Route
	if r.isOpenShift && !wildflyServer.Spec.DisableHTTPRoute {
		if route, err = routes.GetOrCreateNewRoute(wildflyServer, r.client, r.scheme, labelsForWildFly(wildflyServer)); err != nil {
			return reconcile.Result{}, err
		} else if route == nil {
			return reconcile.Result{}, nil
		}
	}

	// Update WildFly Server host status
	updateWildflyServer := false
	if r.isOpenShift && !wildflyServer.Spec.DisableHTTPRoute {
		hosts := make([]string, len(route.Status.Ingress))
		for i, ingress := range route.Status.Ingress {
			hosts[i] = ingress.Host
		}
		if !reflect.DeepEqual(hosts, wildflyServer.Status.Hosts) {
			updateWildflyServer = true
			wildflyServer.Status.Hosts = hosts
			reqLogger.Info("Updating hosts", "WildFlyServer", wildflyServer.Name, "WildflyServer hosts", wildflyServer.Status.Hosts)
		}
	}

	if wildflyServer.Status.ScalingdownPods != numberOfPodsToScaleDown {
		wildflyServer.Status.ScalingdownPods = numberOfPodsToScaleDown
		updateWildflyServer = true
	}
	// Ensure the pod states are up to date by switching it to active when statefulset size follows the wilflyserver spec
	if numberOfPodsToScaleDown <= 0 {
		for k, v := range wildflyServer.Status.Pods {
			if v.State != wildflyv1alpha1.PodStateActive {
				wildflyServer.Status.Pods[k].State = wildflyv1alpha1.PodStateActive
				updateWildflyServer = true
			}
		}
	}

	// Update WildFly Server pod status based on the number of StatefulSet pods
	requeue, podsStatus := getPodStatus(podList.Items, wildflyServer.Status.Pods)
	if !reflect.DeepEqual(podsStatus, wildflyServer.Status.Pods) {
		updateWildflyServer = true
		wildflyServer.Status.Pods = podsStatus
		reqLogger.Info("Updating the pod status with new status", "Pod statuses", podsStatus)
	}

	if updateWildflyServer {
		if err := resources.UpdateStatus(wildflyServer, r.client, wildflyServer); err != nil {
			reqLogger.Error(err, "Failed to update WildFlyServer status.")
			return reconcile.Result{}, err
		}
		requeue = true
	}

	return reconcile.Result{Requeue: requeue}, nil
}

func (r *ReconcileWildFlyServer) checkLoadBalancer(wildflyServer *wildflyv1alpha1.WildFlyServer, loadBalancer *corev1.Service) (mustReconcile bool, mustRequeue bool, err error) {
	sessionAffinity := wildflyServer.Spec.SessionAffinity
	if sessionAffinity && loadBalancer.Spec.SessionAffinity != corev1.ServiceAffinityClientIP {
		if sessionAffinity {
			loadBalancer.Spec.SessionAffinity = corev1.ServiceAffinityClientIP
		} else {
			loadBalancer.Spec.SessionAffinity = corev1.ServiceAffinityNone
		}
		if err := resources.Update(wildflyServer, r.client, loadBalancer); err != nil {
			return true, false, err
		}
		return true, true, nil
	}
	return false, false, nil
}

// checkStatefulSet checks if the statefulset is up to date with the current WildFlyServerSpec.
// it returns true if a reconcile result must be returned.
// the 2nd boolean specifies whether the result must be required.
// A non-nil error if an error happend while updating/deleting the statefulset.
func (r *ReconcileWildFlyServer) checkStatefulSet(wildflyServer *wildflyv1alpha1.WildFlyServer, foundStatefulSet *appsv1.StatefulSet,
	podList *corev1.PodList) (mustReconcile bool, mustRequeue bool, err error) {
	var update, requeue bool
	// Ensure the statefulset replicas is up to date (driven by scaledown processing)
	wildflyServerSpecSize := wildflyServer.Spec.Size
	desiredStatefulSetReplicaSize := wildflyServerSpecSize
	// - for scale up
	if wildflyServerSpecSize > *foundStatefulSet.Spec.Replicas {
		log.Info("Scaling up and updating replica size to "+strconv.Itoa(int(wildflyServerSpecSize)),
			"StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
		foundStatefulSet.Spec.Replicas = &desiredStatefulSetReplicaSize
		update = true
	}
	// - for scale down
	if wildflyServerSpecSize < *foundStatefulSet.Spec.Replicas {
		log.Info("Scaling down statefulset by verification if pods are clean by recovery",
			"StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
		// Change the number of replicas in statefulset, changing based on the pod state
		nameToPodState := make(map[string]string)
		for _, v := range wildflyServer.Status.Pods {
			nameToPodState[v.Name] = v.State
		}
		numberOfPods := len(podList.Items)
		numberOfPodsToShutdown := 0
		// Searching the array of deployed pods from top to down, if the pod with the highest number is clean
		//   then the statefulset replica size can be decreased by 1
		for index := numberOfPods - 1; index >= 0; index-- {
			podItem := podList.Items[index]
			if podStatus, exist := nameToPodState[podItem.Name]; exist {
				if podStatus == wildflyv1alpha1.PodStateScalingDownClean {
					// the pod with the highest number is clean to go
					numberOfPodsToShutdown++
				} else {
					// the pod with the highest number can't be removed, waiting for the next reconcile loop
					break
				}
			}
		}
		// Scaling down statefulset to number of pods that were cleaned by recovery
		calculatedStatefulSetReplicaSize := int32(numberOfPods - numberOfPodsToShutdown)
		desiredStatefulSetReplicaSize = calculatedStatefulSetReplicaSize
		foundStatefulSet.Spec.Replicas = &desiredStatefulSetReplicaSize
		if wildflyServerSpecSize <= calculatedStatefulSetReplicaSize && *foundStatefulSet.Spec.Replicas > calculatedStatefulSetReplicaSize {
			log.Info("Scaling down and updating replica size to "+strconv.Itoa(int(calculatedStatefulSetReplicaSize)),
				"StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
			update = true
		}
		// There are some unclean pods which can't be scaled down
		if wildflyServerSpecSize < calculatedStatefulSetReplicaSize {
			log.Info("Statefulset was not fully scaled to the desired replica size "+strconv.Itoa(int(wildflyServerSpecSize))+
				" while StatefulSet is to be at size "+strconv.Itoa(int(calculatedStatefulSetReplicaSize))+
				". Some pods were not cleaned by recovery. Verify status of the WildflyServer "+wildflyServer.Name,
				"StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
			r.recorder.Event(wildflyServer, corev1.EventTypeWarning, "WildFlyServerScaledown",
				"Transaction recovery slowed down the scaledown.")
			requeue = true
		}
	}

	if generationStr, found := foundStatefulSet.Annotations["wildfly.org/wildfly-server-generation"]; found {
		if generation, err := strconv.ParseInt(generationStr, 10, 64); err == nil {
			// WildFlyServer spec has possibly changed, delete the statefulset
			// so that a new one is created from the updated spec
			log.Info("Comparing generations?", "Annotations", generation, "WildFlyServer", wildflyServer.Generation)
			if generation != wildflyServer.Generation {
				statefulSet := r.statefulSetForWildFly(wildflyServer)
				delete := false
				// changes to VolumeClaimTemplates can not be updated and requires a delete/create of the statefulset
				if len(statefulSet.Spec.VolumeClaimTemplates) > 0 {
					if len(foundStatefulSet.Spec.VolumeClaimTemplates) == 0 {
						// existing stateful set does not have a VCT
						delete = true
					} else {
						foundVCT := foundStatefulSet.Spec.VolumeClaimTemplates[0]
						vct := statefulSet.Spec.VolumeClaimTemplates[0]

						if foundVCT.Name != vct.Name ||
							!reflect.DeepEqual(foundVCT.Spec.AccessModes, vct.Spec.AccessModes) ||
							!reflect.DeepEqual(foundVCT.Spec.Resources, vct.Spec.Resources) {
							delete = true
						}
					}
				} else {
					if len(foundStatefulSet.Spec.VolumeClaimTemplates) != 0 {
						// existing stateful set has a VCT while updated statefulset does not
						delete = true
					}
				}

				if delete {
					// VolumeClaimTemplates has changed, the statefulset can not be updated and must be deleted
					if err = resources.Delete(wildflyServer, r.client, foundStatefulSet); err != nil {
						log.Error(err, "Failed to Delete StatefulSet.", "StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
						return true, false, err
					}
					log.Info("Deleting StatefulSet that is not up to date with the WildFlyServer StorageSpec", "StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
					return true, true, nil
				}

				// all other changes are in the spec Template or Replicas and the statefulset can be updated
				foundStatefulSet.Spec.Template = statefulSet.Spec.Template
				foundStatefulSet.Spec.Replicas = &desiredStatefulSetReplicaSize
				foundStatefulSet.Annotations["wildfly.org/wildfly-server-generation"] = strconv.FormatInt(wildflyServer.Generation, 10)
				if err = resources.Update(wildflyServer, r.client, foundStatefulSet); err != nil {
					log.Error(err, "Failed to Update StatefulSet.", "StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
					return true, false, err
				}
				log.Info("Updating StatefulSet to be up to date with the WildFlyServer Spec", "StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
				return true, true, nil
			}
		}
	}

	if update {
		log.Info("Updating statefulset", "StatefulSet.Replicas", foundStatefulSet.Spec.Replicas)
		err := r.client.Update(context.TODO(), foundStatefulSet)
		if err != nil {
			log.Error(err, "Failed to update StatefulSet.", "StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
			return false, true, err
		}
		return false, true, nil
	}

	return false, requeue, nil
}

// matches checks if the envVar from the WildFlyServerSpec matches the same env var from the container.
// If it does not match, it updates the container EnvVar with the fields from the WildFlyServerSpec and return false.
func matches(container *v1.Container, envVar corev1.EnvVar) bool {
	for index, e := range container.Env {
		if envVar.Name == e.Name {
			if !reflect.DeepEqual(envVar, e) {
				container.Env[index] = envVar
				return false
			}
			return true
		}
	}
	//append new spec env to container's env var
	container.Env = append(container.Env, envVar)
	return false
}

// listing pods which belongs to the WildFly server
//   the pods are differentiated based on the selectors
func (r *ReconcileWildFlyServer) getPodsForWildFly(w *wildflyv1alpha1.WildFlyServer) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(labelsForWildFly(w))
	listOps := &client.ListOptions{
		Namespace:     w.Namespace,
		LabelSelector: labelSelector,
	}
	err := r.client.List(context.TODO(), listOps, podList)

	if err == nil {
		// sorting pods by number in the name
		wildflyutil.SortPodListByName(podList)
	}
	return podList, err
}

// statefulSetForWildFly returns a wildfly StatefulSet object
func (r *ReconcileWildFlyServer) statefulSetForWildFly(w *wildflyv1alpha1.WildFlyServer) *appsv1.StatefulSet {
	ls := labelsForWildFly(w)
	// track the generation number of the WildFlyServer that created the statefulset to ensure that the
	// statefulset is always up to date with the WildFlyServerSpec
	annotations := make(map[string]string)
	annotations["wildfly.org/wildfly-server-generation"] = strconv.FormatInt(w.Generation, 10)

	replicas := w.Spec.Size
	applicationImage := w.Spec.ApplicationImage
	volumeName := w.Name + "-volume"
	labesForActiveWildflyPod := labelsForWildFly(w)
	labesForActiveWildflyPod[markerOperatedByHeadless] = markerServiceActive
	labesForActiveWildflyPod[markerOperatedByLoadbalancer] = markerServiceActive

	statefulSet := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        w.Name,
			Namespace:   w.Namespace,
			Labels:      ls,
			Annotations: annotations,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:            &replicas,
			ServiceName:         services.LoadBalancerServiceName(w),
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labesForActiveWildflyPod,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  w.Name,
						Image: applicationImage,
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: httpApplicationPort,
								Name:          "http",
							},
							{
								ContainerPort: httpManagementPort,
								Name:          "admin",
							},
						},
						LivenessProbe: createLivenessProbe(),
						// Readiness Probe is options
						ReadinessProbe: createReadinessProbe(),
						VolumeMounts: []corev1.VolumeMount{{
							Name:      volumeName,
							MountPath: standaloneServerDataDirPath,
						}},
					}},
					ServiceAccountName: w.Spec.ServiceAccountName,
				},
			},
		},
	}

	if len(w.Spec.EnvFrom) > 0 {
		statefulSet.Spec.Template.Spec.Containers[0].EnvFrom = append(statefulSet.Spec.Template.Spec.Containers[0].EnvFrom, w.Spec.EnvFrom...)
	}

	if len(w.Spec.Env) > 0 {
		statefulSet.Spec.Template.Spec.Containers[0].Env = append(statefulSet.Spec.Template.Spec.Containers[0].Env, w.Spec.Env...)
	}

	// TODO the KUBERNETES_NAMESPACE and KUBERNETES_LABELS env should only be set if
	// the application uses clustering and KUBE_PING.
	statefulSet.Spec.Template.Spec.Containers[0].Env = append(statefulSet.Spec.Template.Spec.Containers[0].Env, envForClustering(labels.SelectorFromSet(ls).String())...)

	storageSpec := w.Spec.Storage

	if storageSpec == nil {
		statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	} else if storageSpec.EmptyDir != nil {
		emptyDir := storageSpec.EmptyDir
		statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: v1.VolumeSource{
				EmptyDir: emptyDir,
			},
		})
	} else {
		pvcTemplate := storageSpec.VolumeClaimTemplate
		if pvcTemplate.Name == "" {
			pvcTemplate.Name = volumeName
		}
		pvcTemplate.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		pvcTemplate.Spec.Resources = storageSpec.VolumeClaimTemplate.Spec.Resources
		pvcTemplate.Spec.Selector = storageSpec.VolumeClaimTemplate.Spec.Selector
		statefulSet.Spec.VolumeClaimTemplates = append(statefulSet.Spec.VolumeClaimTemplates, pvcTemplate)
	}

	standaloneConfigMap := w.Spec.StandaloneConfigMap
	if standaloneConfigMap != nil {
		configMapName := standaloneConfigMap.Name
		configMapKey := standaloneConfigMap.Key
		if configMapKey == "" {
			configMapKey = "standalone.xml"
		}
		log.Info("Reading standalone configuration from configmap", "StandaloneConfigMap.Name", configMapName, "StandaloneConfigMap.Key", configMapKey)

		statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "standalone-config-volume",
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: configMapName,
					},
					Items: []v1.KeyToPath{
						{
							Key:  configMapKey,
							Path: "standalone.xml",
						},
					},
				},
			},
		})
		statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      "standalone-config-volume",
			MountPath: jbossHome + "/standalone/configuration/standalone.xml",
			SubPath:   "standalone.xml",
		})
	}

	// Set WildFlyServer instance as the owner and controller
	controllerutil.SetControllerReference(w, statefulSet, r.scheme)
	return statefulSet
}

func (r *ReconcileWildFlyServer) routeForWildFly(w *wildflyv1alpha1.WildFlyServer) *routev1.Route {
	weight := int32(100)

	route := &routev1.Route{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "route.openshift.io/v1",
			Kind:       "Route",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      w.Name,
			Namespace: w.Namespace,
			Labels:    labelsForWildFly(w),
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   services.LoadBalancerServiceName(w),
				Weight: &weight,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("http"),
			},
		},
	}
	// Set WildFlyServer instance as the owner and controller
	controllerutil.SetControllerReference(w, route, r.scheme)

	return route
}

func (r *ReconcileWildFlyServer) finalizeWildflyServer(reqLogger logr.Logger, w *wildflyv1alpha1.WildFlyServer) error {
	podList, err := r.getPodsForWildFly(w)
	if err != nil {
		return fmt.Errorf("Finalizer processing: failed to list pods for WildflyServer %v:%v name Error: %v", w.Namespace, w.Name, err)
	}
	_, err = r.processTransactionRecoveryScaleDown(reqLogger, w, len(podList.Items), podList)
	if err != nil {
		// error during processing transaction recovery, error from finalizer
		return fmt.Errorf("Finalizer processing: failed transaction recovery for WildflyServer %v:%v name Error: %v", w.Namespace, w.Name, err)
	}
	for _, v := range w.Status.Pods {
		if v.State == wildflyv1alpha1.PodStateScalingDownRecoveryInvestigation {
			return fmt.Errorf("Finalizer processing: recovery in progress at %v/%v at pod %v", w.Namespace, w.Name, v.Name)
		}
		if v.State != wildflyv1alpha1.PodStateScalingDownClean {
			return fmt.Errorf("Finalizer processing: transaction recovery processed with unfinished transactions at %v/%v at pod %v with state %v",
				w.Namespace, w.Name, v.Name, v.State)
		}
	}
	reqLogger.Info("Finalizer finished succesfully", "WildflyServer Namespace", w.Namespace, "WildflyServer Name", w.Name)
	return nil
}

func (r *ReconcileWildFlyServer) addWildflyServerFinalizer(reqLogger logr.Logger, w *wildflyv1alpha1.WildFlyServer) error {
	reqLogger.Info("Adding Finalizer for the WildFlyServer", "Finalizer Name", wildflyServerFinalizer)
	w.SetFinalizers(append(w.GetFinalizers(), wildflyServerFinalizer))

	// Update CR WildflyServer
	if err := resources.Update(w, r.client, w); err != nil {
		reqLogger.Error(err, "Failed to update WildFlyServer with finalizer", "Finalizer Name", wildflyServerFinalizer)
		return err
	}
	return nil
}

// setLabelAsDisabled returns true when kubernetes etcd was updated with label names on some pods, otherwise false
//  returns error when error processing happened at some of the pods, otherwise if no error occurs then nil is returned
func (r *ReconcileWildFlyServer) setLabelAsDisabled(w *wildflyv1alpha1.WildFlyServer, reqLogger logr.Logger, labelName string, numberOfPodsToScaleDown int,
	podList *corev1.PodList, podNameToState map[string]string, desiredPodState string) (bool, error) {
	wildflyServerNumberOfPods := len(podList.Items)

	var resultError error
	errStrings := ""
	updated := false

	for scaleDownIndex := 1; scaleDownIndex <= numberOfPodsToScaleDown; scaleDownIndex++ {
		scaleDownPod := podList.Items[wildflyServerNumberOfPods-scaleDownIndex]
		scaleDownPodName := scaleDownPod.ObjectMeta.Name
		// updating the label on pod only if the pod is in particular one state
		if podNameToState == nil || podNameToState[scaleDownPodName] == desiredPodState {
			kubernetesUpdated, err := r.updatePodLabel(w, &scaleDownPod, labelName, markerServiceDisabled)
			if err != nil {
				errStrings += " [[" + err.Error() + "]],"
			}
			if kubernetesUpdated {
				reqLogger.Info("Label for pod succesfully updated", "Pod Name", scaleDownPod.ObjectMeta.Name,
					"Label name", labelName, "Label value", markerServiceDisabled)
				updated = true
			}
		}
	}

	if errStrings != "" {
		resultError = fmt.Errorf("%s", errStrings)
	}
	return updated, resultError
}

// processTransactionRecoveryScaleDown runs transaction recovery on provided number of pods
//   needUpdate returns true if some changes in status pods were done and client update is expected
//   mustReconcileRequeue returns true if the reconcile requeue loop should be called as soon as possible
//   err reports error which occurs during method processing
func (r *ReconcileWildFlyServer) processTransactionRecoveryScaleDown(reqLogger logr.Logger, w *wildflyv1alpha1.WildFlyServer,
	numberOfPodsToScaleDown int, podList *corev1.PodList) (mustReconcileRequeue bool, err error) {

	wildflyServerNumberOfPods := len(podList.Items)
	scaleDownPodsStates := sync.Map{} // map referring to: pod name - pod state
	scaleDownErrors := sync.Map{}     // errors occured during processing the scaledown for the pods

	// Setting-up the pod status - status is used to decide if the pod could be scaled (aka. removed from the statefulset)
	var updated bool
	for scaleDownIndex := 1; scaleDownIndex <= numberOfPodsToScaleDown; scaleDownIndex++ {
		scaleDownPodName := podList.Items[wildflyServerNumberOfPods-scaleDownIndex].ObjectMeta.Name
		wildflyServerSpecPodStatus := getWildflyServerPodStatusByName(w, scaleDownPodName)
		if wildflyServerSpecPodStatus == nil {
			continue
		}
		if wildflyServerSpecPodStatus.State == wildflyv1alpha1.PodStateActive {
			wildflyServerSpecPodStatus.State = wildflyv1alpha1.PodStateScalingDownRecoveryInvestigation
			scaleDownPodsStates.Store(scaleDownPodName, wildflyv1alpha1.PodStateScalingDownRecoveryInvestigation)
			updated = true
		} else {
			scaleDownPodsStates.Store(scaleDownPodName, wildflyServerSpecPodStatus.State)
		}
	}
	if updated { // updating status of pods as soon as possible
		w.Status.ScalingdownPods = int32(numberOfPodsToScaleDown)
		err := r.client.Status().Update(context.TODO(), w)
		if err != nil {
			err = fmt.Errorf("There was trouble to update state of WildflyServer: %v, error: %v", w.Status.Pods, err)
		}
		return true, err
	}

	var wg sync.WaitGroup
	for scaleDownIndex := 1; scaleDownIndex <= numberOfPodsToScaleDown; scaleDownIndex++ {
		scaleDownPod := podList.Items[wildflyServerNumberOfPods-scaleDownIndex]
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Scaledown scenario, need to handle transction recovery
			scaleDownPodName := scaleDownPod.ObjectMeta.Name
			scaleDownPodIP := scaleDownPod.Status.PodIP
			if strings.Contains(scaleDownPodIP, ":") && !strings.HasPrefix(scaleDownPodIP, "[") {
				scaleDownPodIP = "[" + scaleDownPodIP + "]" // for IPv6
			}

			podState, ok := scaleDownPodsStates.Load(scaleDownPodName)
			if !ok {
				scaleDownErrors.Store(scaleDownPodName,
					fmt.Errorf("Cannot find pod name '%v' in the list of the active pods for the WildflyServer operator: %v",
						scaleDownPodName, w.ObjectMeta.Name))
				return
			}

			if podState != wildflyv1alpha1.PodStateScalingDownClean {
				reqLogger.Info("Transaction recovery scaledown processing", "Pod Name", scaleDownPodName, "IP Address", scaleDownPodIP)

				// TODO: check if it's possible to set graceful shutdown here
				success, message, err := r.checkRecovery(reqLogger, &scaleDownPod, w)
				if err != nil {
					// An error happened during recovery
					scaleDownErrors.Store(scaleDownPodName, err)
					return
				}
				if success {
					// Recovery was processed with success, the pod is clean to go
					scaleDownPodsStates.Store(scaleDownPodName, wildflyv1alpha1.PodStateScalingDownClean)
				} else if message != "" {
					// Some in-doubt transaction left in store, the pod is still dirty
					reqLogger.Info("In-doubt transactions in object store", "Pod Name", scaleDownPodName, "Message", message)
					scaleDownPodsStates.Store(scaleDownPodName, wildflyv1alpha1.PodStateScalingDownRecoveryDirty)
					// Let's setup recovery peroperties, restart the app server and thus speed-up the recovery
					r.setupRecoveryPropertiesAndRestart(reqLogger, &scaleDownPod, w)
				}
			}
		}() // execution of the go routine for one pod
	}
	wg.Wait()

	// Updating the pod state based on the recovery processing when a scale down is in progress
	updated = false
	for wildflyServerPodStatusIndex, v := range w.Status.Pods {
		if podStateValue, exist := scaleDownPodsStates.Load(v.Name); exist {
			if w.Status.Pods[wildflyServerPodStatusIndex].State != podStateValue.(string) {
				updated = true
			}
			w.Status.Pods[wildflyServerPodStatusIndex].State = podStateValue.(string)
		}
	}
	// Verification if an error happened during the recovery processing
	var errStrings string
	numberOfScaleDownErrors := 0
	var resultError error
	scaleDownErrors.Range(func(k, v interface{}) bool {
		numberOfScaleDownErrors++
		errStrings += " [[" + v.(error).Error() + "]],"
		return true
	})
	if numberOfScaleDownErrors > 0 {
		resultError = fmt.Errorf("Found %v errors:\n%s", numberOfScaleDownErrors, errStrings)
		r.recorder.Event(w, corev1.EventTypeWarning, "WildFlyServerScaledown",
			"Errors during transaction recovery scaledown processing. Consult operator log.")
	}

	if updated { // recovery changed the state of the pods
		w.Status.ScalingdownPods = int32(numberOfPodsToScaleDown)
		err := r.client.Status().Update(context.TODO(), w)
		if err != nil {
			err = fmt.Errorf("Error to update state of WildflyServer after recovery processing for pods %v, "+
				"error: %v. Recovery processing errors: %v", w.Status.Pods, err, resultError)
		}
		return true, err
	}

	return false, resultError
}

func (r *ReconcileWildFlyServer) checkRecovery(reqLogger logr.Logger, scaleDownPod *corev1.Pod, w *wildflyv1alpha1.WildFlyServer) (bool, string, error) {
	scaleDownPodName := scaleDownPod.ObjectMeta.Name
	scaleDownPodIP := scaleDownPod.Status.PodIP
	scaleDownPodRecoveryPort := defaultRecoveryPort

	// Reading timestamp for the latest log record
	scaleDownPodLogTimestampAtStart, err := wildflyutil.ObtainLogLatestTimestamp(scaleDownPod)
	if err != nil {
		if scaleDownPod.Status.Phase != corev1.PodRunning {
			return false, "", fmt.Errorf("Pod '%s' of WildflyServer '%v' could not be ready for reading log during scaling down, "+
				"please verify its state. Cause error: %v", scaleDownPodName, w.ObjectMeta.Name, err)
		}
		return false, "", fmt.Errorf("Failed to read log from scaling down pod '%v', error: %v", scaleDownPodName, err)
	}

	// If we are in state of recovery is needed the setup of the server has to be already done
	if scaleDownPod.Annotations[markerRecoveryPort] == "" {
		reqLogger.Info("Enabling recovery listener for processing scaledown at " + scaleDownPodName)
		// Enabling recovery listener to be able to call the server for the recovery
		jsonResult, err := wildflyutil.ExecuteMgmtOp(scaleDownPod, jbossHome, wildflyutil.MgmtOpTxnEnableRecoveryListener)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "cannot execute") {
				reqLogger.Error(err, "Verify if operator JBOSS_HOME variable determines the place where the application server is installed",
					w.Name+".JBOSS_HOME", jbossHome)
			}
			return false, "", fmt.Errorf("Cannot enable transaction recovery listener for scaling down pod %v, error: %v", scaleDownPodName, err)
		}
		if !wildflyutil.IsMgmtOutcomeSuccesful(jsonResult) {
			return false, "", fmt.Errorf("Failed to enable transaction recovery listener for scaling down pod %v. Scaledown processing cannot trigger recovery. "+
				"Management command: %v, JSON response: %v", scaleDownPodName, wildflyutil.MgmtOpTxnEnableRecoveryListener, jsonResult)
		}
		// Enabling the recovery listner may require the server being reloaded
		isReloadRequired := wildflyutil.ReadJSONDataByIndex(jsonResult["response-headers"], "operation-requires-reload")
		if isReloadRequiredBool, _ := strconv.ParseBool(isReloadRequired); isReloadRequiredBool {
			reqLogger.Info("Reloading WildFly for recovery listener being activated at pod " + scaleDownPodName)
			wildflyutil.ExecuteOpAndWaitForServerBeingReady(wildflyutil.MgmtOpReload, scaleDownPod, jbossHome)
		}

		// Reading recovery port from the app server with management port
		reqLogger.Info("Query to find the transaction recovery port to force scan at pod " + scaleDownPodName)
		queriedScaleDownPodRecoveryPort, err := wildflyutil.GetTransactionRecoveryPort(scaleDownPod, jbossHome)
		if err == nil && queriedScaleDownPodRecoveryPort != 0 {
			scaleDownPodRecoveryPort = queriedScaleDownPodRecoveryPort
		}
		if err != nil {
			reqLogger.Error(err, "Error on reading transaction recovery port with management command", "Pod name", scaleDownPodName)
		}

		// Marking the pod as being searched for the recovery port already
		scaleDownPod.Annotations[markerRecoveryPort] = strconv.FormatInt(int64(scaleDownPodRecoveryPort), 10)
		if err := resources.Update(w, r.client, scaleDownPod); err != nil {
			return false, "", fmt.Errorf("Failed to update pod annotations, pod name %v, annotations to be set %v, error: %v",
				scaleDownPodName, scaleDownPod.Annotations, err)
		}
	} else {
		// pod annotation already contains information on recovery port thus we just use it
		queriedScaleDownPodRecoveryPortString := scaleDownPod.Annotations[markerRecoveryPort]
		queriedScaleDownPodRecoveryPort, err := strconv.Atoi(queriedScaleDownPodRecoveryPortString)
		if err != nil {
			delete(scaleDownPod.Annotations, markerRecoveryPort)
			if err := resources.Update(w, r.client, scaleDownPod); err != nil {
				reqLogger.Info("Cannot update scaledown pod %v while resetting the annotation map to %v", scaleDownPodName, scaleDownPod.Annotations)
			}
			return false, "", fmt.Errorf("Cannot convert recovery port value '%s' to integer for the scaling down pod %v, error: %v",
				queriedScaleDownPodRecoveryPortString, scaleDownPodName, err)
		}
		scaleDownPodRecoveryPort = int32(queriedScaleDownPodRecoveryPort)
	}

	// With enabled recovery listener and the port, let's start the recovery scan
	reqLogger.Info("Executing recovery scan at "+scaleDownPodName, "Pod IP", scaleDownPodIP, "Recovery port", scaleDownPodRecoveryPort)
	_, err = wildflyutil.SocketConnect(scaleDownPodIP, scaleDownPodRecoveryPort, txnRecoveryScanCommand)
	if err != nil {
		return false, "", fmt.Errorf("Failed to run transaction recovery scan for scaling down pod %v. "+
			"Please, verify the pod log file. Error: %v", scaleDownPodName, err)
	}
	// No error on recovery scan => all the registered resources were available during the recovery processing
	foundLogLine, err := wildflyutil.VerifyLogContainsRegexp(scaleDownPod, scaleDownPodLogTimestampAtStart, recoveryErrorRegExp)
	if err != nil {
		return false, "", fmt.Errorf("Cannot parse log from scaling down pod %v, error: %v", scaleDownPodName, err)
	}
	if foundLogLine != "" {
		retString := fmt.Sprintf("Scale down transaction recovery processing contains errors in log. The recovery will be retried."+
			"Pod name: %v, log line with error '%v'", scaleDownPod, foundLogLine)
		return false, retString, nil
	}
	// Probing transaction log to verify there is not in-doubt transaction in the log
	_, err = wildflyutil.ExecuteMgmtOp(scaleDownPod, jbossHome, wildflyutil.MgmtOpTxnProbe)
	if err != nil {
		return false, "", fmt.Errorf("Error in probing transaction log for scaling down pod %v, error: %v", scaleDownPodName, err)
	}
	// Transaction log was probed, now we read the set of transactions which are in-doubt
	jsonResult, err := wildflyutil.ExecuteMgmtOp(scaleDownPod, jbossHome, wildflyutil.MgmtOpTxnRead)
	if err != nil {
		return false, "", fmt.Errorf("Cannot read transactions from the transaction log for pod scaling down %v, error: %v", scaleDownPodName, err)
	}
	if !wildflyutil.IsMgmtOutcomeSuccesful(jsonResult) {
		return false, "", fmt.Errorf("Cannot get list of the in-doubt transactions at pod %v for transaction scaledown", scaleDownPodName)
	}
	// Is the number of in-doubt transactions equal to zero?
	transactions := jsonResult["result"]
	txnMap, isMap := transactions.(map[string]interface{}) // typing the variable to be a map of interfaces
	if isMap && len(txnMap) > 0 {
		retString := fmt.Sprintf("Recovery scan to be invoked as the transaction log storage is not empty for pod scaling down pod %v, "+
			"transaction list: %v", scaleDownPodName, txnMap)
		return false, retString, nil
	}
	// Verification of the unfinished data of the WildFly transaction client (verification of the directory content)
	lsCommand := fmt.Sprintf(`ls %s/%s/ 2> /dev/null || true`, standaloneServerDataDirPath, wftcDataDirName)
	commandResult, err := wildflyutil.ExecRemote(scaleDownPod, lsCommand)
	if err != nil {
		return false, "", fmt.Errorf("Cannot query filesystem at scaling down pod %v to check existing remote transactions. "+
			"Exec command: %v", scaleDownPodName, lsCommand)
	}
	if commandResult != "" {
		retString := fmt.Sprintf("WildFly Transaction Client data dir is not empty and scaling down of the pod '%v' will be retried."+
			"Wildfly Transacton Client data dir path '%v', output listing: %v",
			scaleDownPodName, standaloneServerDataDirPath+"/"+wftcDataDirName, commandResult)
		return false, retString, nil
	}
	return true, "", nil
}

func (r *ReconcileWildFlyServer) setupRecoveryPropertiesAndRestart(reqLogger logr.Logger, scaleDownPod *corev1.Pod, w *wildflyv1alpha1.WildFlyServer) (bool, error) {
	scaleDownPodName := scaleDownPod.ObjectMeta.Name

	// Setup and restart only if it was not done in the prior reconcile cycle
	if scaleDownPod.Annotations[markerRecoveryPropertiesSetup] == "" {
		setBackoffPeriodOp := fmt.Sprintf(wildflyutil.MgmtOpSystemPropertyRecoveryBackoffPeriod, "1")
		setOrphanIntervalOp := fmt.Sprintf(wildflyutil.MgmtOpSystemPropertyOrphanSafetyInterval, "1")
		setRecoveryPeriodOp := fmt.Sprintf(wildflyutil.MgmtOpSystemPropertyPeriodicRecoveryPeriod, "1")
		wildflyutil.ExecuteMgmtOp(scaleDownPod, jbossHome, setBackoffPeriodOp)
		wildflyutil.ExecuteMgmtOp(scaleDownPod, jbossHome, setOrphanIntervalOp)
		jsonResult, err := wildflyutil.ExecuteMgmtOp(scaleDownPod, jbossHome, setRecoveryPeriodOp)
		if err != nil {
			return false, fmt.Errorf("Error to setup recovery periods by system properties with management operation '%s' for scaling down pod %s, error: %v",
				setRecoveryPeriodOp, scaleDownPodName, err)
		}
		if !wildflyutil.IsMgmtOutcomeSuccesful(jsonResult) {
			return false, fmt.Errorf("Setting up the recovery period by system property with operation '%s' at pod %s was not succesful. JSON output: %v",
				setRecoveryPeriodOp, scaleDownPodName, jsonResult)
		}

		if _, err := wildflyutil.ExecuteOpAndWaitForServerBeingReady(wildflyutil.MgmtOpRestart, scaleDownPod, jbossHome); err != nil {
			return false, fmt.Errorf("Cannot restart application server after setting up the periodic recovery properties, error: %v", err)
		}

		// Marking the pod as the server was already setup for the recovery
		scaleDownPod.Annotations[markerRecoveryPropertiesSetup] = "true"
		if err := resources.Update(w, r.client, scaleDownPod); err != nil {
			return false, fmt.Errorf("Failed to update pod annotations, pod name %v, annotations to be set %v, error: %v",
				scaleDownPodName, scaleDownPod.Annotations, err)
		}
	} else {
		reqLogger.Info("Recovery properties for app server at pod were already defined. Skipping server restart.", "Pod Name", scaleDownPodName)
	}
	return true, nil
}

func (r *ReconcileWildFlyServer) updatePodLabel(w *wildflyv1alpha1.WildFlyServer, scaleDownPod *corev1.Pod, labelName, labelValue string) (bool, error) {
	updated := false
	if scaleDownPod.ObjectMeta.Labels[labelName] != labelValue {
		scaleDownPod.ObjectMeta.Labels[labelName] = labelValue
		if err := resources.Update(w, r.client, scaleDownPod); err != nil {
			return false, fmt.Errorf("Failed to update pod labels for pod %v with label [%s=%s], error: %v",
				scaleDownPod.ObjectMeta.Name, labelName, labelValue, err)
		}
		updated = true
	}
	return updated, nil
}

func getWildflyServerPodStatusByName(w *wildflyv1alpha1.WildFlyServer, podName string) *wildflyv1alpha1.PodStatus {
	for index, podStatus := range w.Status.Pods {
		if podName == podStatus.Name {
			return &w.Status.Pods[index]
		}
	}
	return nil
}

// getPodStatus returns the pod names of the array of pods passed in
func getPodStatus(pods []corev1.Pod, originalPodStatuses []wildflyv1alpha1.PodStatus) (bool, []wildflyv1alpha1.PodStatus) {
	var requeue = false
	var podStatuses []wildflyv1alpha1.PodStatus
	podStatusesOriginalMap := make(map[string]wildflyv1alpha1.PodStatus)
	for _, v := range originalPodStatuses {
		podStatusesOriginalMap[v.Name] = v
	}
	for _, pod := range pods {
		podState := wildflyv1alpha1.PodStateActive
		if value, exists := podStatusesOriginalMap[pod.Name]; exists {
			podState = value.State
		}
		podStatuses = append(podStatuses, wildflyv1alpha1.PodStatus{
			Name:  pod.Name,
			PodIP: pod.Status.PodIP,
			State: podState,
		})
		if pod.Status.PodIP == "" {
			requeue = true
		}
	}
	return requeue, podStatuses
}

// createLivenessProbe create a Exec probe if the SERVER_LIVENESS_SCRIPT env var is present.
// Otherwise, it creates a HTTPGet probe that checks the /health endpoint on the admin port.
//
// If defined, the SERVER_LIVENESS_SCRIPT env var must be the path of a shell script that
// complies to the Kuberenetes probes requirements.
func createLivenessProbe() *corev1.Probe {
	livenessProbeScript, defined := os.LookupEnv("SERVER_LIVENESS_SCRIPT")
	if defined {
		return &corev1.Probe{
			Handler: corev1.Handler{
				Exec: &v1.ExecAction{
					Command: []string{"/bin/bash", "-c", livenessProbeScript},
				},
			},
			InitialDelaySeconds: 60,
		}
	}
	return &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &v1.HTTPGetAction{
				Path: "/health",
				Port: intstr.FromString("admin"),
			},
		},
		InitialDelaySeconds: 60,
	}
}

// createReadinessProbe create a Exec probe if the SERVER_READINESS_SCRIPT env var is present.
// Otherwise, it returns nil (i.e. no readiness probe is configured).
//
// If defined, the SERVER_READINESS_SCRIPT env var must be the path of a shell script that
// complies to the Kuberenetes probes requirements.
func createReadinessProbe() *corev1.Probe {
	readinessProbeScript, defined := os.LookupEnv("SERVER_READINESS_SCRIPT")
	if defined {
		return &corev1.Probe{
			Handler: corev1.Handler{
				Exec: &v1.ExecAction{
					Command: []string{"/bin/bash", "-c", readinessProbeScript},
				},
			},
		}
	}
	return nil
}

func envForClustering(labels string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "KUBERNETES_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.namespace",
				},
			},
		},
		{
			Name:  "KUBERNETES_LABELS",
			Value: labels,
		},
	}
}

func labelsForWildFly(w *wildflyv1alpha1.WildFlyServer) map[string]string {
	labels := make(map[string]string)
	labels["app.kubernetes.io/name"] = w.Name
	labels["app.kubernetes.io/managed-by"] = os.Getenv("LABEL_APP_MANAGED_BY")
	labels["app.openshift.io/runtime"] = os.Getenv("LABEL_APP_RUNTIME")
	if w.Labels != nil {
		for labelKey, labelValue := range w.Labels {
			labels[labelKey] = labelValue
		}
	}
	return labels
}

// errorIsMatchesForKind return true if the error is that there is no matches for the kind & version
func errorIsNoMatchesForKind(err error, kind string, version string) bool {
	return strings.HasPrefix(err.Error(), fmt.Sprintf("no matches for kind \"%s\" in version \"%s\"", kind, version))
}

// isOpenShift returns true when the container platform is detected as OpenShift
func isOpenShift(c *rest.Config) bool {
	isOpenShift, err := openshiftutils.IsOpenShift(c)
	if err != nil {
		return false
	}
	return isOpenShift
}

// isWildFlyFinalizer returns true when is defined the finalizer should be added
func isWildFlyFinalizer() bool {
	isWildFlyFinalizer, err := strconv.ParseBool(wildflyFinalizerEnv)
	if err != nil {
		isWildFlyFinalizer = true
	}
	return isWildFlyFinalizer
}
