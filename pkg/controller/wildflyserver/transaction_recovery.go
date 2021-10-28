package wildflyserver

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	"github.com/tevino/abool"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	wfly "github.com/wildfly/wildfly-operator/pkg/controller/util"
	"github.com/wildfly/wildfly-operator/pkg/resources"
	corev1 "k8s.io/api/core/v1"
)

var (
	recoveryErrorRegExp = regexp.MustCompile("ERROR.*Periodic Recovery")
)

const (
	markerServiceDisabled       = "disabled" // label value for the pod that's clean from scaledown and it should be removed from service
	defaultRecoveryPort   int32 = 4712
	markerRecoveryPort          = "recovery-port" // annotation name to save recovery port
	// markerRecoveryPropertiesSetup declare that recovery properties were setup for app server
	markerRecoveryPropertiesSetup = "recovery-properties-setup"
	txnRecoveryScanCommand        = "SCAN"            // Narayana socket command to force recovery
	wftcDataDirName               = "ejb-xa-recovery" // data directory where WFTC stores transaction runtime data
)

// Defines a new type to map the result of the checkRecovery routine
type checkResult int

// Declaration of the possible results of checkRecovery
const (
	clean checkResult = iota
	recovery
	heuristic
	genericError
)

func (r *ReconcileWildFlyServer) checkRecovery(reqLogger logr.Logger, scaleDownPod *corev1.Pod, w *wildflyv1alpha1.WildFlyServer) (checkResult, string, error) {
	scaleDownPodName := scaleDownPod.ObjectMeta.Name
	scaleDownPodIP := scaleDownPod.Status.PodIP
	scaleDownPodRecoveryPort := defaultRecoveryPort

	// Reading timestamp of the latest log record
	scaleDownPodLogTimestampAtStart, err := wfly.RemoteOps.ObtainLogLatestTimestamp(scaleDownPod)
	if err != nil {
		return genericError, "", fmt.Errorf("The pod '%s' (of the WildflyServer '%v') is not ready to be scaled down, "+
			"please verify its state. Error: %v", scaleDownPodName, w.ObjectMeta.Name, err)
	}

	// If transactions needs to be recovered, the setup of the server has to be carried out
	if scaleDownPod.Annotations[markerRecoveryPort] == "" {
		reqLogger.Info("Verification that the transaction recovery listener of the pod " + scaleDownPodName + "is ready to recover transactions")
		// Verify the recovery listener is setup
		jsonResult, err := wfly.ExecuteMgmtOp(scaleDownPod, wfly.MgmtOpTxnCheckRecoveryListener)
		if err != nil {
			return genericError, "", fmt.Errorf("Cannot check if the transaction recovery listener of the pod %v is enabled, error: %v", scaleDownPodName, err)
		}
		if !wfly.IsMgmtOutcomeSuccesful(jsonResult) {
			return genericError, "", fmt.Errorf("Failed to verify if the transaction recovery listener of the pod %v is enabled. Scaledown processing cannot trigger recovery. "+
				"Management command: %v, JSON response: %v", scaleDownPodName, wfly.MgmtOpTxnCheckRecoveryListener, jsonResult)
		}
		// When the transaction recovery listener is not enabled then the pod will be terminated
		isTxRecoveryListenerEnabledAsInterface := wfly.ReadJSONDataByIndex(jsonResult, "result")
		isTxRecoveryListenerEnabledAsString, _ := wfly.ConvertToString(isTxRecoveryListenerEnabledAsInterface)
		if txrecoverydefined, err := strconv.ParseBool(isTxRecoveryListenerEnabledAsString); err == nil && !txrecoverydefined {
			reqLogger.Info("The transaction recovery listener of the pod " + scaleDownPodName + "is not enabled")
			r.recorder.Event(w, corev1.EventTypeWarning, "WildFlyServerTransactionRecovery",
				"The transaction recovery listener of the pod "+scaleDownPodName+" is not defined and the recovery process cannot started."+
					" If this is not intentional, fix the server configuration. The pod will be terminated.")
			return clean, "", nil
		}

		// Reading recovery port from the app server with management port
		reqLogger.Info("Query the app server to find out the transaction recovery port of the pod " + scaleDownPodName)
		queriedScaleDownPodRecoveryPort, err := wfly.GetTransactionRecoveryPort(scaleDownPod)
		if err == nil && queriedScaleDownPodRecoveryPort != 0 {
			scaleDownPodRecoveryPort = queriedScaleDownPodRecoveryPort
		}
		if err != nil {
			reqLogger.Error(err, "Error reading the transaction recovery port of the pod "+scaleDownPodName, "The default port "+string(scaleDownPodRecoveryPort)+" will be used.")
		}

		// Save the recovery port into the annotations
		annotations := wfly.MapMerge(
			scaleDownPod.GetAnnotations(), map[string]string{markerRecoveryPort: strconv.FormatInt(int64(scaleDownPodRecoveryPort), 10)})
		scaleDownPod.SetAnnotations(annotations)
		if err := resources.Update(w, r.client, scaleDownPod); err != nil {
			return genericError, "", fmt.Errorf("Failed to update the annotation of the pod %v, annotations to be set %v, error: %v",
				scaleDownPodName, scaleDownPod.Annotations, err)
		}
	} else {
		// The annotations of the pod already contain information about the recovery port
		queriedScaleDownPodRecoveryPortString := scaleDownPod.Annotations[markerRecoveryPort]
		queriedScaleDownPodRecoveryPort, err := strconv.Atoi(queriedScaleDownPodRecoveryPortString)
		if err != nil {
			delete(scaleDownPod.Annotations, markerRecoveryPort)
			if errUpdate := resources.Update(w, r.client, scaleDownPod); errUpdate != nil {
				reqLogger.Info("Cannot update the pod while resetting the recovery port annotation",
					"Pod", scaleDownPodName, "Annotations", scaleDownPod.Annotations, "Error", errUpdate)
			}
			return genericError, "", fmt.Errorf("Cannot convert recovery port value '%s' to integer for the pod %v, error: %v",
				queriedScaleDownPodRecoveryPortString, scaleDownPodName, err)
		}
		scaleDownPodRecoveryPort = int32(queriedScaleDownPodRecoveryPort)
	}

	// Transaction recovery listener is enabled and the recovery port has been retrieved, the recovery scan can be started
	reqLogger.Info("Executing the recovery scan for the pod "+scaleDownPodName, "Pod IP", scaleDownPodIP, "Recovery port", scaleDownPodRecoveryPort)
	_, err = wfly.RemoteOps.SocketConnect(scaleDownPodIP, scaleDownPodRecoveryPort, txnRecoveryScanCommand)
	if err != nil {
		delete(scaleDownPod.Annotations, markerRecoveryPort)
		if errUpdate := r.client.Update(context.TODO(), scaleDownPod); errUpdate != nil {
			reqLogger.Info("Cannot update the pod while resetting the recovery port annotation",
				"Pod", scaleDownPodName, "Annotations", scaleDownPod.Annotations, "Error", errUpdate)
		}
		return genericError, "", fmt.Errorf("Failed to run the transaction recovery scan for the pod %v. "+
			"Please, verify the pod logs. Error: %v", scaleDownPodName, err)
	}
	// No error on recovery scan => all the registered resources were available during the recovery processing
	foundLogLine, err := wfly.RemoteOps.VerifyLogContainsRegexp(scaleDownPod, scaleDownPodLogTimestampAtStart, recoveryErrorRegExp)
	if err != nil {
		return genericError, "", fmt.Errorf("Cannot parse logs from the pod %v, error: %v", scaleDownPodName, err)
	}
	if foundLogLine != "" {
		retString := fmt.Sprintf("The transaction recovery process contains errors in the logs. The recovery will be retried."+
			"Pod: %v, log line with error '%v'", scaleDownPod, foundLogLine)
		return genericError, retString, nil
	}
	// Probing transaction logs to verify there is not any in-doubt transaction in the log
	_, err = wfly.ExecuteMgmtOp(scaleDownPod, wfly.MgmtOpTxnProbe)
	if err != nil {
		return genericError, "", fmt.Errorf("Error while probing transaction logs of the pod %v, error: %v", scaleDownPodName, err)
	}
	// Transaction logs were probed, now we read the set of in-doubt transactions
	jsonResult, err := wfly.ExecuteMgmtOp(scaleDownPod, wfly.MgmtOpTxnRead)
	if err != nil {
		return genericError, "", fmt.Errorf("Cannot read transactions from the log store of the pod %v, error: %v", scaleDownPodName, err)
	}
	if !wfly.IsMgmtOutcomeSuccesful(jsonResult) {
		return genericError, "", fmt.Errorf("Cannot get the list of in-doubt transactions of the pod %v", scaleDownPodName)
	}
	// Is the number of in-doubt transactions equal to zero?
	transactions := jsonResult["result"]
	txnMap, isMap := transactions.(map[string]interface{}) // typing the variable to be a map of interfaces
	if isMap && len(txnMap) > 0 {

		// Check for HEURISTIC transactions
		jsonResult, err := wfly.ExecuteMgmtOp(scaleDownPod, wfly.MgmtOpTxnReadHeuristic)
		if err != nil {
			return genericError, "", fmt.Errorf("Cannot read HEURISTIC transactions from the log store of the pod %v, error: %v", scaleDownPodName, err)
		}
		if !wfly.IsMgmtOutcomeSuccesful(jsonResult) {
			return genericError, "", fmt.Errorf("Cannot read HEURISTIC transactions from the log store of the pod %v", scaleDownPodName)
		}
		// Is the number of HEURISTIC transactions equal to zero?
		transactions := jsonResult["result"]
		heuristicTxnArray, isArray := transactions.([]interface{}) // typing the variable to be an array of interfaces
		if isArray && len(heuristicTxnArray) > 0 {
			retString := fmt.Sprintf("There are HEURISTIC transactions in the log store of the pod %v. Please, resolve them manually, "+
				"transaction list: %v", scaleDownPodName, heuristicTxnArray)
			return heuristic, retString, nil
		}

		retString := fmt.Sprintf("A recovery scan is needed as the log store of the pod %v is not empty, "+
			"transaction list: %v", scaleDownPodName, txnMap)
		return recovery, retString, nil
	}
	// Verification of the unfinished data of the WildFly transaction client (verification of the directory content)
	lsCommand := fmt.Sprintf(`ls ${JBOSS_HOME}/%s/%s/ 2> /dev/null || true`, resources.StandaloneServerDataDirRelativePath, wftcDataDirName)
	commandResult, err := wfly.RemoteOps.Execute(scaleDownPod, lsCommand)
	if err != nil {
		return genericError, "", fmt.Errorf("Cannot query the filesystem of the pod %v to check existing remote transactions. "+
			"Exec command: %v", scaleDownPodName, lsCommand)
	}
	if commandResult != "" {
		retString := fmt.Sprintf("WildFly's data directory is not empty and the scaling down of the pod '%v' will be retried."+
			"Wildfly's data directory path is: '${JBOSS_HOME}/%v/%v', output listing: %v",
			scaleDownPodName, resources.StandaloneServerDataDirRelativePath, wftcDataDirName, commandResult)
		return recovery, retString, nil
	}
	return clean, "", nil
}

func (r *ReconcileWildFlyServer) setupRecoveryPropertiesAndRestart(reqLogger logr.Logger, scaleDownPod *corev1.Pod, w *wildflyv1alpha1.WildFlyServer) (mustReconcile int, err error) {
	scaleDownPodName := scaleDownPod.ObjectMeta.Name
	mustReconcile = requeueOff

	// Pod is not started or not fully operable. For safely marking it clean by recovery we need to have working CLI operations.
	if scaleDownPod.Status.Phase != corev1.PodRunning {
		reqLogger.Info("Pod is not running. It will be hopefully started in a while. "+
			"Transaction recovery needs the pod being fully started to be capable to mark it as clean for the scale down.",
			"Pod Name", scaleDownPodName, "Object Name", w.ObjectMeta.Name, "Pod Phase", scaleDownPod.Status.Phase)
		return requeueLater, nil
	}
	if isRunning, errCli := wfly.IsAppServerRunningViaJBossCli(scaleDownPod); !isRunning {
		reqLogger.Info("Pod is labeled as running but the JBoss CLI cannot. It will be hopefully accessible in a while for "+
			"transaction recovery may proceed with scale down.", "Pod Name", scaleDownPodName, "Object Name", w.ObjectMeta.Name,
			"Error", errCli)
		return requeueLater, nil
	}

	// Setup and restart only if it was not done in the prior reconcile cycle
	if scaleDownPod.Annotations[markerRecoveryPropertiesSetup] == "" {
		reqLogger.Info("Setting up back-off period and orphan detection properties for scaledown transaction recovery", "Pod Name", scaleDownPodName)
		setPeriodOps := fmt.Sprintf(wfly.MgmtOpSystemPropertyRecoveryBackoffPeriod, "1") + "," + fmt.Sprintf(wfly.MgmtOpSystemPropertyOrphanSafetyInterval, "1")
		disableTMOp := fmt.Sprintf(wfly.MgmtOpSystemPropertyTransactionManagerDisabled, "true")
		jsonResult, errExecution := wfly.ExecuteMgmtOp(scaleDownPod, setPeriodOps+","+disableTMOp)

		// command may end-up with error with status 'rolled-back' which means duplication, thus do not check for error as error could be a positive outcome
		isOperationRolledBack := wfly.ReadJSONDataByIndex(jsonResult, "rolled-back")
		isOperationRolledBackAsString, _ := wfly.ConvertToString(isOperationRolledBack)
		if !wfly.IsMgmtOutcomeSuccesful(jsonResult) && isOperationRolledBackAsString != "true" {
			return requeueLater, fmt.Errorf("Error on setting up the back-off and orphan detection periods. "+
				"The jboss-cli.sh operation '%s' at pod %s failed. JSON output: %v, command error: %v",
				setPeriodOps, scaleDownPodName, jsonResult, errExecution)
		}

		reqLogger.Info("Setting system property 'org.wildfly.internal.cli.boot.hook.marker.dir' at '/tmp/markerdir/wf-cli-shutdown-initiated'", "Pod Name", scaleDownPodName)
		wfly.RemoteOps.Execute(scaleDownPod, "mkdir /tmp/markerdir && touch /tmp/markerdir/wf-cli-shutdown-initiated || true")
		wfly.ExecuteMgmtOp(scaleDownPod, "/system-property=org.wildfly.internal.cli.boot.hook.marker.dir:add(value=/tmp/markerdir)")

		reqLogger.Info("Restarting application server to apply the env properties", "Pod Name", scaleDownPodName)
		if err := wfly.ExecuteOpAndWaitForServerBeingReady(reqLogger, wfly.MgmtOpRestart, scaleDownPod); err != nil {
			resources.Delete(w, r.client, scaleDownPod)
			return requeueNow, fmt.Errorf("Cannot restart application server at pod %v after setting up the periodic recovery properties. "+
				"The pod was deleted to be restarted for new recovery attempt. Error: %v", scaleDownPodName, err)
		}

		reqLogger.Info("Marking pod as being setup for transaction recovery. Adding annotation "+markerRecoveryPropertiesSetup, "Pod Name", scaleDownPodName)
		annotations := wfly.MapMerge(
			scaleDownPod.GetAnnotations(), map[string]string{markerRecoveryPropertiesSetup: "true"})
		scaleDownPod.SetAnnotations(annotations)
		if err := resources.Update(w, r.client, scaleDownPod); err != nil {
			return requeueNow, fmt.Errorf("Failed to update pod annotations, pod name %v, annotations to be set %v, error: %v",
				scaleDownPodName, scaleDownPod.Annotations, err)
		}

		return requeueOff, nil
	}
	reqLogger.Info("Recovery properties at pod were already defined. Skipping server restart.", "Pod Name", scaleDownPodName)
	return requeueOff, nil
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

/*
 * processTransactionRecoveryScaleDown runs transaction recovery on provided number of pods
 * Returns the int contant mustReconcile:
 * - 'requeueNow' if the reconcile requeue loop should be called as soon as possible
 * - 'requeueLater' if requeue loop is needed but it could be delayed
 * - 'requeueOff' if requeue loop is not necessary
 * err reports error that occured during the transaction recovery
 */
func (r *ReconcileWildFlyServer) processTransactionRecoveryScaleDown(reqLogger logr.Logger, w *wildflyv1alpha1.WildFlyServer,
	numberOfPodsToScaleDown int, podList *corev1.PodList) (mustReconcile int, err error) {

	// podList comes from the wildflyserver_controller.Reconcile method,
	// where the list of pods specific to a WildFlyServer CR is created

	// Current number of pods
	wildflyServerNumberOfPods := len(podList.Items)
	// sync.map to store errors occurred during processing the scaledown of pods
	errorsSyncMap := sync.Map{}
	// Return value for the reconcile cycle
	mustReconcile = requeueOff

	if wildflyServerNumberOfPods == 0 || numberOfPodsToScaleDown == 0 {
		// no active pods to scale down or no pods are requested to scale down
		return requeueOff, nil
	}
	// In case the WildFlyServer custom resource has not been updated yet, wait the next reconcile cycle
	if len(w.Status.Pods) != wildflyServerNumberOfPods {
		return requeueLater, nil
	}
	if w.Spec.BootableJar {
		reqLogger.Info("Transaction scale down recovery is unsupported for Bootable JAR pods. The pods will be removed without checking pending transactions.",
			"Number of pods to be scaled down", numberOfPodsToScaleDown)
		return r.skipRecoveryAndForceScaleDown(w, wildflyServerNumberOfPods, numberOfPodsToScaleDown, podList)
	}
	if w.Spec.DeactivateTransactionRecovery {
		reqLogger.Info("The 'DeactivateTransactionRecovery' flag is set to true thus the process to recovery transactions will be skipped.")
		return r.skipRecoveryAndForceScaleDown(w, wildflyServerNumberOfPods, numberOfPodsToScaleDown, podList)
	}
	subsystemsList, err := wfly.ListSubsystems(&podList.Items[0])
	if err != nil {
		reqLogger.Info("Cannot get list of subsystems available in application server", "Pod", podList.Items[0].ObjectMeta.Name)
		return requeueLater, err
	}
	if !wfly.ContainsInList(subsystemsList, "transactions") {
		reqLogger.Info("Transaction scale down recovery will be skipped as it's not available for application server without transaction subsystem.",
			"Number of pods to be scaled down", numberOfPodsToScaleDown)
		return r.skipRecoveryAndForceScaleDown(w, wildflyServerNumberOfPods, numberOfPodsToScaleDown, podList)
	}
	if w.Spec.Storage == nil || w.Spec.Storage.EmptyDir != nil {
		isJDBCObjectStore, err := isJDBCLogStoreInUse(&podList.Items[0])
		if err != nil {
			reqLogger.Info("Cannot find if transaction subsystems uses JDBC object store", "Pod", podList.Items[0].ObjectMeta.Name)
			return requeueLater, err
		}
		if !isJDBCObjectStore {
			reqLogger.Info("Transaction scale down recovery will be skipped as it is unsupported when WildFlyServer's Storage is 'EmptyDir' "+
				"and the transaction subsystem does not use the JDBC object store. As a consequence, the transaction recovery processing is unsafe. "+
				"Please, configure WildFlyServer.Spec.Storage with a Persistent Volume Claim or use a database to store transaction logs.",
				"Number of pods to be scaled down", numberOfPodsToScaleDown)
			return r.skipRecoveryAndForceScaleDown(w, wildflyServerNumberOfPods, numberOfPodsToScaleDown, podList)
		}
	}

	// Flag to signal that there are pods in need of updating
	podStatusNeedsUpdating := abool.New()
	// Select pods to be scaled down (starting from the tail of the list)
	var podsToScaleDown []corev1.Pod
	for index := wildflyServerNumberOfPods - 1; index >= wildflyServerNumberOfPods-numberOfPodsToScaleDown; index-- {
		// Only running pods can be considered
		if podList.Items[index].Status.Phase == "Running" {
			podsToScaleDown = append(podsToScaleDown, podList.Items[index])
		}
	}
	if len(podsToScaleDown) == 0 {
		reqLogger.Info("There are not 'Running' pods to scale down")
		return requeueLater, nil
	}
	// PodStatus.State is set to PodStateScalingDownRecoveryInvestigation (SCALING_DOWN_RECOVERY_INVESTIGATION)
	// to start the scale down process
	for _, corePod := range podsToScaleDown {
		// wildflyServerSpecPodStatus points to wildflyv1alpha1.WildFlyServer.Status.Pods[index];
		wildflyServerSpecPodStatus := getWildflyServerPodStatusByName(w, corePod.ObjectMeta.Name)
		if wildflyServerSpecPodStatus.State == wildflyv1alpha1.PodStateActive {
			wildflyServerSpecPodStatus.State = wildflyv1alpha1.PodStateScalingDownRecoveryInvestigation
			podStatusNeedsUpdating.Set()
		}
	}
	// If there are pods in PodStateScalingDownRecoveryInvestigation state, an update cycle must be run
	if podStatusNeedsUpdating.IsSet() {
		w.Status.ScalingdownPods = int32(numberOfPodsToScaleDown)
		err := resources.UpdateWildFlyServerStatus(w, r.client)
		if err != nil {
			return requeueNow, fmt.Errorf("Failed to update the state of the WildflyServer resource: %v, error: %v", w.Status.Pods, err)
		}
	}

	// Reset the flag to signal the need to run an update cycle
	podStatusNeedsUpdating.UnSet()
	var wg sync.WaitGroup
	for _, corePod := range podsToScaleDown {
		// As the variables `wildflyServerSpecPodStatus` and `corePodTemp` are created for each iteration,
		// only the goroutine defined in the same iteration can access them. In this way, the situation where
		//multiple goroutines access the same variable is avoided.
		wildflyServerSpecPodStatus := getWildflyServerPodStatusByName(w, corePod.ObjectMeta.Name)
		corePodTemp := corePod
		wg.Add(1)
		go func() {
			defer wg.Done()
			podIP := wildflyServerSpecPodStatus.PodIP
			if strings.Contains(podIP, ":") && !strings.HasPrefix(podIP, "[") {
				podIP = "[" + podIP + "]" // for IPv6
			}

			if wildflyServerSpecPodStatus.State != wildflyv1alpha1.PodStateScalingDownClean {
				reqLogger.Info("Transaction recovery scaledown processing", "Pod Name", corePodTemp.ObjectMeta.Name,
					"IP Address", podIP, "Pod State", wildflyServerSpecPodStatus.State, "Pod's Recovery Counter", wildflyServerSpecPodStatus.RecoveryCounter)

				// For full and correct recovery we need to run two recovery checks with orphan detection interval set to minimum
				needsReconcile, setupErr := r.setupRecoveryPropertiesAndRestart(reqLogger, &corePodTemp, w)
				if needsReconcile > mustReconcile {
					mustReconcile = needsReconcile
				}
				if setupErr != nil {
					errorsSyncMap.Store(corePodTemp.ObjectMeta.Name, setupErr)
					return
				}
				//The futher recovery attempts won't succeed until reconcilation loop is repeated
				if needsReconcile != requeueOff {
					return
				}

				// Running the recovery check twice to discover in-doubt transactions
				var (
					outcome     checkResult
					message     string
					recoveryErr error
				)
				for count := 0; count < 2; count++ {
					outcome, message, recoveryErr = r.checkRecovery(reqLogger, &corePodTemp, w)
					// This if handles the outcome genericError
					if recoveryErr != nil {
						errorsSyncMap.Store(corePodTemp.ObjectMeta.Name, recoveryErr)
						return
					}
				}
				if outcome == clean {
					// Recovery was processed with success, the pod is clean to go
					if wildflyServerSpecPodStatus.State != wildflyv1alpha1.PodStateScalingDownClean {
						wildflyServerSpecPodStatus.State = wildflyv1alpha1.PodStateScalingDownClean
						podStatusNeedsUpdating.Set()
					}
				} else if outcome == recovery {
					reqLogger.Info("Pod "+corePodTemp.ObjectMeta.Name+" is trying to recover unfinished transactions", "Message", message)
					if wildflyServerSpecPodStatus.State != wildflyv1alpha1.PodStateScalingDownRecoveryProcessing {
						wildflyServerSpecPodStatus.State = wildflyv1alpha1.PodStateScalingDownRecoveryProcessing
					}
					wildflyServerSpecPodStatus.RecoveryCounter++
					reqLogger.Info("The recovery counter of the pod " + corePodTemp.ObjectMeta.Name + " is: " + strconv.Itoa(int(wildflyServerSpecPodStatus.RecoveryCounter)))
					// As the RecoveryCounter increases at every recovery attempt, podStatusNeedsUpdating must be set to trigger the update of the status
					podStatusNeedsUpdating.Set()
				} else if outcome == heuristic {
					if wildflyServerSpecPodStatus.State != wildflyv1alpha1.PodStateScalingDownRecoveryHeuristic {
						wildflyServerSpecPodStatus.State = wildflyv1alpha1.PodStateScalingDownRecoveryHeuristic
						podStatusNeedsUpdating.Set()
					}
					reqLogger.Info("Pod "+corePodTemp.ObjectMeta.Name+" has heuristic transactions", "Message", message)
					r.recorder.Event(w, corev1.EventTypeWarning, "Transaction Recovery", "There are HEURISTIC transactions in "+corePodTemp.ObjectMeta.Name+"! Please, resolve them manually!")
				}
			}
		}() // execution of the go routine for one pod
	}
	wg.Wait()

	// Verification if an error happened during the recovery processing
	var errStrings string
	numberOfScaleDownErrors := 0
	var resultError error
	errorsSyncMap.Range(func(k, v interface{}) bool {
		numberOfScaleDownErrors++
		errStrings += " [[" + v.(error).Error() + "]],"
		return true
	})
	if numberOfScaleDownErrors > 0 {
		resultError = fmt.Errorf("Found %v errors:\n%s", numberOfScaleDownErrors, errStrings)
		r.recorder.Event(w, corev1.EventTypeWarning, "WildFlyServerScaledown",
			"Errors during transaction recovery scaledown processing. Consult operator log.")
	}

	if podStatusNeedsUpdating.IsSet() { // recovery changed the state of the pods
		w.Status.ScalingdownPods = int32(numberOfPodsToScaleDown)
		err := resources.UpdateWildFlyServerStatus(w, r.client)
		if err != nil {
			return requeueNow, fmt.Errorf("Error to update state of WildflyServer after recovery processing for pods %v, "+
				"error: %v. Recovery processing errors: %v", w.Status.Pods, err, resultError)
		}
	}

	return mustReconcile, resultError
}

// setLabelAsDisabled returns true when label was updated or an issue on update requires recociliation
//  returns error when an error occurs during processing, otherwise if no error occurs nil is returned
func (r *ReconcileWildFlyServer) setLabelAsDisabled(w *wildflyv1alpha1.WildFlyServer, reqLogger logr.Logger, labelName string, numberOfPodsToScaleDown int,
	podList *corev1.PodList) (bool, error) {
	wildflyServerNumberOfPods := len(podList.Items)

	var resultError error
	errStrings := ""
	reconciliationNeeded := false

	for scaleDownIndex := 1; scaleDownIndex <= numberOfPodsToScaleDown; scaleDownIndex++ {
		if wildflyServerNumberOfPods-scaleDownIndex >= len(podList.Items) || wildflyServerNumberOfPods-scaleDownIndex < 0 {
			return false, fmt.Errorf("Cannot update pod label for pod number %v as there is no such active pod in list: %v, ",
				wildflyServerNumberOfPods-scaleDownIndex, podList)
		}
		scaleDownPod := podList.Items[wildflyServerNumberOfPods-scaleDownIndex]
		updateRequireReconciliation, err := r.updatePodLabel(w, &scaleDownPod, labelName, markerServiceDisabled)
		if err != nil {
			errStrings += " [[" + err.Error() + "]],"
		}
		if err == nil && updateRequireReconciliation {
			reqLogger.Info("Label for pod successfully updated", "Pod Name", scaleDownPod.ObjectMeta.Name,
				"Label name", labelName, "Label value", markerServiceDisabled)
			reconciliationNeeded = true
		}
	}

	if errStrings != "" {
		resultError = fmt.Errorf("%s", errStrings)
		reconciliationNeeded = true
	}
	return reconciliationNeeded, resultError
}

// isJDBCLogStoreInUse executes jboss CLI command to search if transctions are saved in the JDBC object store
//   i.e. the transaction log is not stored in the file system but out of the StatefulSet controlled $JBOSS_HOME /data directory
func isJDBCLogStoreInUse(pod *corev1.Pod) (bool, error) {
	isJDBCTxnStore, err := wfly.ExecuteAndGetResult(pod, wfly.MgmtOpTxnCheckJdbcStore)
	if err != nil {
		return false, fmt.Errorf("Failed to verify if the transaction subsystem uses JDBC object store. Error: %v", err)
	}
	isJDBCTxnStoreAsString, err := wfly.ConvertToString(isJDBCTxnStore)
	if err != nil {
		return false, fmt.Errorf("Failed to verify if the transaction subsystem uses JDBC object store. Error: %v", err)
	}
	return strconv.ParseBool(isJDBCTxnStoreAsString)
}

// skipRecoveryAndForceScaleDown serves to sets the scaling down pods as being processed by recovery and mark them
//   to be deleted by statefulset update. This is used for cases when recovery process should be skipped.
func (r *ReconcileWildFlyServer) skipRecoveryAndForceScaleDown(w *wildflyv1alpha1.WildFlyServer, totalNumberOfPods int,
	numberOfPodsToScaleDown int, podList *corev1.PodList) (mustReconcile int, err error) {
	for scaleDownIndex := 1; scaleDownIndex <= numberOfPodsToScaleDown; scaleDownIndex++ {
		scaleDownPodName := podList.Items[totalNumberOfPods-scaleDownIndex].ObjectMeta.Name
		wildflyServerSpecPodStatus := getWildflyServerPodStatusByName(w, scaleDownPodName)
		if wildflyServerSpecPodStatus == nil {
			continue
		}
		wildflyServerSpecPodStatus.State = wildflyv1alpha1.PodStateScalingDownClean
	}
	w.Status.ScalingdownPods = int32(numberOfPodsToScaleDown)
	// process update on the WildFlyServer resource
	err = resources.UpdateWildFlyServerStatus(w, r.client)
	if err != nil {
		log.Error(err, "Error on updating WildFlyServer when skipping recovery scale down")
	}
	return requeueOff, nil
}
