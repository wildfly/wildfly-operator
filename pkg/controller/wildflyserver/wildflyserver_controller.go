package wildflyserver

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	wildflyutil "github.com/wildfly/wildfly-operator/pkg/controller/util"
	"github.com/wildfly/wildfly-operator/pkg/resources"
	"github.com/wildfly/wildfly-operator/pkg/resources/routes"
	"github.com/wildfly/wildfly-operator/pkg/resources/servicemonitors"
	"github.com/wildfly/wildfly-operator/pkg/resources/services"
	"github.com/wildfly/wildfly-operator/pkg/resources/statefulsets"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	routev1 "github.com/openshift/api/route/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	openshiftutils "github.com/RHsyseng/operator-utils/pkg/utils/openshift"
)

const (
	controllerName = "wildflyserver-controller"
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
		client:      mgr.GetClient(),
		scheme:      mgr.GetScheme(),
		isOpenShift: isOpenShift(mgr.GetConfig()),
		recorder:    mgr.GetEventRecorderFor(controllerName),
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
	if hasServiceMonitor() {
		if err = c.Watch(&source.Kind{Type: &monitoringv1.ServiceMonitor{}}, &enqueueRequestForOwner); err != nil {
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

	// If statefulset was deleted during processing recovery scaledown the number of replicas in WildflyServer spec
	//  does not defines the number of pods which should be left active until recovered
	desiredReplicaSizeForNewStatefulSet := wildflyServer.Spec.Replicas + wildflyServer.Status.ScalingdownPods

	// Check if the statefulSet already exists, if not create a new one
	statefulSet, err := statefulsets.GetOrCreateNewStatefulSet(wildflyServer, r.client, r.scheme,
		LabelsForWildFly(wildflyServer), desiredReplicaSizeForNewStatefulSet)
	if err != nil {
		return reconcile.Result{}, err
	} else if statefulSet == nil {
		return reconcile.Result{Requeue: true}, nil
	}

	// List of pods which belongs under this WildflyServer instance
	podList, err := GetPodsForWildFly(r, wildflyServer)
	if err != nil {
		reqLogger.Error(err, "Failed to list pods.", "WildFlyServer.Namespace", wildflyServer.Namespace, "WildFlyServer.Name", wildflyServer.Name)
		return reconcile.Result{}, err
	}
	wildflyServerSpecSize := wildflyServer.Spec.Replicas
	statefulsetSpecSize := *statefulSet.Spec.Replicas
	numberOfDeployedPods := int32(len(podList.Items))
	numberOfPodsToScaleDown := statefulsetSpecSize - wildflyServerSpecSize // difference between desired pod count and the current number of pods

	// if the number of desired replica size (aka. WildflyServer.Spec.Replicas) is different from the number of active pods
	//  and the statefulset replica size was already changed to follow the value defined by the wildflyserver spec then wait for sts to reconcile
	if statefulsetSpecSize == wildflyServerSpecSize && numberOfDeployedPods != wildflyServerSpecSize {
		reqLogger.Info("Number of pods does not match the WildFlyServer specification. Waiting to get numbers in sync.",
			"WildflyServer specification", wildflyServer.Name, "Expected number of pods", wildflyServerSpecSize, "Number of deployed pods", numberOfDeployedPods,
			"StatefulSet spec size", statefulsetSpecSize)
		return reconcile.Result{Requeue: true}, nil
	}
	// the recovers scaledown process requires all pods will be active and running otherwise it's not able to clean them
	if numberOfDeployedPods < statefulsetSpecSize {
		reqLogger.Info("Number of pods is lower than the StatefulSet replica size. Waiting to get number in sync.",
			"Number of deployed pods", numberOfDeployedPods, "StatefulSet spec size", statefulsetSpecSize)
		return reconcile.Result{Requeue: true}, nil
	}

	// Processing scaled down
	//   updating scaling-down pods for not being requests through loadbalancer
	updated, err := r.setLabelAsDisabled(wildflyServer, reqLogger, resources.MarkerOperatedByLoadbalancer, int(numberOfPodsToScaleDown), podList, nil, "")
	if updated || err != nil { // labels were updated (updated == true) or some error occured (err != nil)
		return reconcile.Result{Requeue: updated}, err
	}
	// Processing recovery on pods which are planned to be removed because of scale down is in progress now
	mustReconcile, err := r.processTransactionRecoveryScaleDown(reqLogger, wildflyServer, int(numberOfPodsToScaleDown), podList)
	if mustReconcile { // server state was updated (or/and some error could happen), we need to reconcile
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
		return reconcile.Result{Requeue: true}, nil
	}
	// Check if the headless service already exists, if not create a new one
	if headlessService, err := services.CreateOrUpdateHeadlessService(wildflyServer, r.client, r.scheme, LabelsForWildFly(wildflyServer)); err != nil {
		return reconcile.Result{}, err
	} else if headlessService == nil {
		return reconcile.Result{Requeue: true}, nil
	}
	// Check if the admin service already exists, if not create a new one
	if adminService, err := services.CreateOrUpdateAdminService(wildflyServer, r.client, r.scheme, LabelsForWildFly(wildflyServer)); err != nil {
		return reconcile.Result{}, err
	} else if adminService == nil {
		return reconcile.Result{Requeue: true}, nil
	}

	// Check if the HTTP route must be created
	var route *routev1.Route
	if r.isOpenShift {
		if !wildflyServer.Spec.DisableHTTPRoute {
			if route, err = routes.GetOrCreateNewRoute(wildflyServer, r.client, r.scheme, LabelsForWildFly(wildflyServer)); err != nil {
				return reconcile.Result{}, err
			} else if route == nil {
				return reconcile.Result{Requeue: true}, nil
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

	// create a Prometheus ServiceMonitor (if the resource exists on the cluster)
	if hasServiceMonitor() {
		if serviceMonitor, err := servicemonitors.GetOrCreateNewServiceMonitor(wildflyServer, r.client, r.scheme, LabelsForWildFly(wildflyServer)); err != nil {
			return reconcile.Result{}, err
		} else if serviceMonitor == nil {
			return reconcile.Result{Requeue: true}, nil
		}
	}

	// Update WildFly Server host status
	updateWildflyServer := false
	if r.isOpenShift {
		if !wildflyServer.Spec.DisableHTTPRoute {
			hosts := make([]string, len(route.Status.Ingress))
			for i, ingress := range route.Status.Ingress {
				hosts[i] = ingress.Host
			}
			if !reflect.DeepEqual(hosts, wildflyServer.Status.Hosts) {
				updateWildflyServer = true
				wildflyServer.Status.Hosts = hosts
				reqLogger.Info("Updating hosts", "WildFlyServer", wildflyServer.Name, "WildflyServer hosts", wildflyServer.Status.Hosts)
			}
		} else {
			// if HTTP routes have been disabled, remove the hosts field from the status
			if len(wildflyServer.Status.Hosts) > 0 {
				updateWildflyServer = true
				wildflyServer.Status.Hosts = nil
				reqLogger.Info("Removing hosts", "WildFlyServer", wildflyServer.Name)
			}
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

	if wildflyServer.Status.Replicas != statefulSet.Status.Replicas {
		wildflyServer.Status.Replicas = statefulSet.Status.Replicas
		updateWildflyServer = true
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
	wildflyServerSpecSize := wildflyServer.Spec.Replicas
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
			log.Info("Statefulset was not scaled to the desired replica size "+strconv.Itoa(int(wildflyServerSpecSize))+
				" (current StatefulSet size: "+strconv.Itoa(int(calculatedStatefulSetReplicaSize))+
				"). Transaction recovery scaledown process has not cleaned all pods. Please, check status of the WildflyServer "+wildflyServer.Name,
				"StatefulSet.Namespace", foundStatefulSet.Namespace, "StatefulSet.Name", foundStatefulSet.Name)
			r.recorder.Event(wildflyServer, corev1.EventTypeWarning, "WildFlyServerScaledown",
				"Transaction recovery slowed down the scaledown.")
			requeue = true
		}
	}

	if !resources.IsCurrentGeneration(wildflyServer, foundStatefulSet) {
		statefulSet := statefulsets.NewStatefulSet(wildflyServer, LabelsForWildFly(wildflyServer), desiredStatefulSetReplicaSize)
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
		if err = resources.Update(wildflyServer, r.client, foundStatefulSet); err != nil {
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

	listOpts := []client.ListOption{
		client.InNamespace(w.Namespace),
		client.MatchingLabels(LabelsForWildFly(w)),
	}
	err := r.client.List(context.TODO(), podList, listOpts...)

	if err == nil {
		// sorting pods by number in the name
		wildflyutil.SortPodListByName(podList)
	}
	return podList, err
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

// hasServiceMonitor checks if ServiceMonitor kind is registered in the cluster.
func hasServiceMonitor() bool {
	return resources.CustomResourceDefinitionExists(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: monitoringv1.Version,
		Kind:    monitoringv1.ServiceMonitorsKind,
	})
}
