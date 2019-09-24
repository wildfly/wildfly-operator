package wildflyserver

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	wildflyutil "github.com/wildfly/wildfly-operator/pkg/controller/util"
	"github.com/wildfly/wildfly-operator/pkg/resources"
	"github.com/wildfly/wildfly-operator/pkg/resources/routes"
	"github.com/wildfly/wildfly-operator/pkg/resources/services"
	"github.com/wildfly/wildfly-operator/pkg/resources/statefulsets"

	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	openshiftutils "github.com/RHsyseng/operator-utils/pkg/utils/openshift"
)

const (
	controllerName         = "wildflyserver-controller"
	wildflyServerFinalizer = "finalizer.wildfly.org"
)

var (
	log = logf.Log.WithName("wildflyserver_controller")
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
		isWildFlyFinalizer: isWildFlyFinalizerEnabled(),
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
	statefulSet, err := statefulsets.GetOrCreateNewStatefulSet(wildflyServer, r.client, r.scheme, LabelsForWildFly(wildflyServer))
	if err != nil {
		return reconcile.Result{}, err
	} else if statefulSet == nil {
		return reconcile.Result{Requeue: true}, nil
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
			requeue, err := r.finalizeWildFlyServer(reqLogger, wildflyServer)
			if err != nil {
				return reconcile.Result{}, err
			}
			if requeue {
				return reconcile.Result{Requeue: true}, nil
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
		if err := r.addWildFlyServerFinalizer(reqLogger, wildflyServer); err != nil {
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
	podList, err := GetPodsForWildFly(r, wildflyServer)
	if err != nil {
		reqLogger.Error(err, "Failed to list pods.", "WildFlyServer.Namespace", wildflyServer.Namespace, "WildFlyServer.Name", wildflyServer.Name)
		return reconcile.Result{}, err
	}
	wildflyServerSpecSize := wildflyServer.Spec.Size
	statefulsetSpecSize := *statefulSet.Spec.Replicas
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
	updated, err := r.setLabelAsDisabled(wildflyServer, reqLogger, resources.MarkerOperatedByLoadbalancer, int(numberOfPodsToScaleDown), podList, nil, "")
	if updated || err != nil { // labels were updated (updated == true) or some error occured (err != nil)
		return reconcile.Result{Requeue: updated}, err
	}
	// Processing recovery on pods which are planned to be removed because of scale down is in progress now
	mustReconcile, err := r.processTransactionRecoveryScaleDown(reqLogger, wildflyServer, int(numberOfPodsToScaleDown), podList)
	if mustReconcile { // server state was updated (or/and some erro could happen), we need to reconcile
		return reconcile.Result{Requeue: true}, err
	}
	if err != nil {
		reqLogger.Error(err, "Failures during scaling down recovery processing", "Desired replica size", wildflyServerSpecSize,
			"Number of pods to be removed", numberOfPodsToScaleDown)
	}

	mustReconcile, err = r.checkStatefulSet(wildflyServer, statefulSet, podList)
	if mustReconcile {
		return reconcile.Result{Requeue: true}, err
	} else if err != nil {
		return reconcile.Result{}, err
	}

	// Check if the loadbalancer already exists, if not create a new one
	loadBalancer, err := services.CreateOrUpdateLoadBalancerService(wildflyServer, r.client, r.scheme, LabelsForWildFly(wildflyServer))
	if err != nil {
		return reconcile.Result{}, err
	} else if loadBalancer == nil {
		return reconcile.Result{}, nil
	}
	// Check if the headless service already exists, if not create a new one
	if headlessService, err := services.CreateOrUpdateHeadlessService(wildflyServer, r.client, r.scheme, LabelsForWildFly(wildflyServer)); err != nil {
		return reconcile.Result{}, err
	} else if headlessService == nil {
		return reconcile.Result{}, nil
	}

	// Check if the HTTP route must be created
	var route *routev1.Route
	if r.isOpenShift {
		if !wildflyServer.Spec.DisableHTTPRoute {
			if route, err = routes.GetOrCreateNewRoute(wildflyServer, r.client, r.scheme, LabelsForWildFly(wildflyServer)); err != nil {
				return reconcile.Result{}, err
			} else if route == nil {
				return reconcile.Result{}, nil
			}
		} else {
			// delete the route that may have been created by a previous generation of the WildFlyServer
			if deleted, err := routes.DeleteExistingRoute(wildflyServer, r.client); err != nil {
				return reconcile.Result{}, err
			} else if deleted {
				return reconcile.Result{}, nil
			}
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
		if err := resources.UpdateWildFlyServerStatus(wildflyServer, r.client); err != nil {
			reqLogger.Error(err, "Failed to update WildFlyServer status.")
			return reconcile.Result{}, err
		}
		requeue = true
	}

	return reconcile.Result{Requeue: requeue}, nil
}

// checkStatefulSet checks if the statefulset is up to date with the current WildFlyServerSpec.
// it returns true if a reconcile result must be returned.
// A non-nil error if an error happend while updating/deleting the statefulset.
func (r *ReconcileWildFlyServer) checkStatefulSet(wildflyServer *wildflyv1alpha1.WildFlyServer, foundStatefulSet *appsv1.StatefulSet,
	podList *corev1.PodList) (mustReconcile bool, err error) {
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
		if wildflyServerSpecSize <= calculatedStatefulSetReplicaSize && *foundStatefulSet.Spec.Replicas > calculatedStatefulSetReplicaSize {
			log.Info("Scaling down and updating replica size to "+strconv.Itoa(int(calculatedStatefulSetReplicaSize)),
				"StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
			foundStatefulSet.Spec.Replicas = &desiredStatefulSetReplicaSize
			update = true
		}
		// There are some unclean pods which can't be scaled down
		if wildflyServerSpecSize < calculatedStatefulSetReplicaSize {
			log.Info("Statefulset was not fully scaled to the desired replica size "+strconv.Itoa(int(wildflyServerSpecSize))+
				" (current StatefulSet size: "+strconv.Itoa(int(calculatedStatefulSetReplicaSize))+
				"). Some pods were not cleaned by recovery. Verify status of the WildflyServer "+wildflyServer.Name,
				"StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
			r.recorder.Event(wildflyServer, corev1.EventTypeWarning, "WildFlyServerScaledown",
				"Transaction recovery slowed down the scaledown.")
			requeue = true
		}
	}

	if !resources.IsCurrentGeneration(wildflyServer, foundStatefulSet) {
		statefulSet := statefulsets.NewStatefulSet(wildflyServer, LabelsForWildFly(wildflyServer))
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
				return true, err
			}
			log.Info("Deleting StatefulSet that is not up to date with the WildFlyServer StorageSpec", "StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
			return true, nil
		}

		// all other changes are in the spec Template or Replicas and the statefulset can be updated
		foundStatefulSet.Spec.Template = statefulSet.Spec.Template
		foundStatefulSet.Spec.Replicas = &desiredStatefulSetReplicaSize
		foundStatefulSet.Annotations[resources.MarkerServerGeneration] = strconv.FormatInt(wildflyServer.Generation, 10)
		if err = resources.Update(wildflyServer, r.client, foundStatefulSet); err != nil {
			log.Error(err, "Failed to Update StatefulSet.", "StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
			return true, err
		}
		log.Info("Updating StatefulSet to be up to date with the WildFlyServer Spec", "StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
		return true, nil
	}

	if update {
		log.Info("Updating statefulset", "StatefulSet.Replicas", foundStatefulSet.Spec.Replicas)
		err := r.client.Update(context.TODO(), foundStatefulSet)
		if err != nil {
			log.Error(err, "Failed to update StatefulSet.", "StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
			return true, err
		}
		return true, nil
	}

	return requeue, nil
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

// GetPodsForWildFly lists pods which belongs to the WildFly server
//   the pods are differentiated based on the selectors
func GetPodsForWildFly(r *ReconcileWildFlyServer, w *wildflyv1alpha1.WildFlyServer) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(LabelsForWildFly(w))
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

func (r *ReconcileWildFlyServer) finalizeWildFlyServer(reqLogger logr.Logger, w *wildflyv1alpha1.WildFlyServer) (bool, error) {
	podList, err := GetPodsForWildFly(r, w)
	if err != nil {
		return false, fmt.Errorf("Finalizer processing: failed to list pods for WildflyServer %v:%v name Error: %v", w.Namespace, w.Name, err)
	}
	requeue, err := r.processTransactionRecoveryScaleDown(reqLogger, w, len(podList.Items), podList)
	if err != nil {
		// error during processing transaction recovery, error from finalizer
		return false, fmt.Errorf("Finalizer processing: failed transaction recovery for WildflyServer %v:%v name Error: %v", w.Namespace, w.Name, err)
	}
	if requeue {
		return true, nil
	}
	for _, v := range w.Status.Pods {
		if v.State == wildflyv1alpha1.PodStateScalingDownRecoveryInvestigation {
			return false, fmt.Errorf("Finalizer processing: recovery in progress at %v/%v at pod %v", w.Namespace, w.Name, v.Name)
		}
		if v.State != wildflyv1alpha1.PodStateScalingDownClean {
			return false, fmt.Errorf("Finalizer processing: transaction recovery processed with unfinished transactions at %v/%v at pod %v with state %v",
				w.Namespace, w.Name, v.Name, v.State)
		}
	}
	reqLogger.Info("Finalizer finished succesfully", "WildflyServer Namespace", w.Namespace, "WildflyServer Name", w.Name)
	return false, nil
}

func (r *ReconcileWildFlyServer) addWildFlyServerFinalizer(reqLogger logr.Logger, w *wildflyv1alpha1.WildFlyServer) error {
	reqLogger.Info("Adding Finalizer for the WildFlyServer", "Finalizer Name", wildflyServerFinalizer)
	w.SetFinalizers(append(w.GetFinalizers(), wildflyServerFinalizer))

	// Update CR WildflyServer
	if err := resources.Update(w, r.client, w); err != nil {
		reqLogger.Error(err, "Failed to update WildFlyServer with finalizer", "Finalizer Name", wildflyServerFinalizer)
		return err
	}
	return nil
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

// LabelsForWildFly return a map of labels that are used for identification
//  of objects belonging to the particular WildflyServer instance
func LabelsForWildFly(w *wildflyv1alpha1.WildFlyServer) map[string]string {
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
func isWildFlyFinalizerEnabled() bool {
	isWildFlyFinalizer, err := strconv.ParseBool(os.Getenv("ENABLE_WILDFLY_FINALIZER"))
	if err != nil {
		return true
	}
	return isWildFlyFinalizer
}
