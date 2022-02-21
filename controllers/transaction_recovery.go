package controllers

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	"github.com/tevino/abool"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/api/v1alpha1"
	"github.com/wildfly/wildfly-operator/pkg/resources"
	wfly "github.com/wildfly/wildfly-operator/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func (r *WildFlyServerReconciler) checkRecovery(reqLogger logr.Logger, scaleDownPod *corev1.Pod, w *wildflyv1alpha1.WildFlyServer) (bool, string, error) {
	scaleDownPodName := scaleDownPod.ObjectMeta.Name
	scaleDownPodIP := scaleDownPod.Status.PodIP
	scaleDownPodRecoveryPort := defaultRecoveryPort

	// Reading timestamp for the latest log record
	scaleDownPodLogTimestampAtStart, err := wfly.RemoteOps.ObtainLogLatestTimestamp(scaleDownPod)
	if err != nil {
		return false, "", fmt.Errorf("Log of the pod '%s' of WildflyServer '%v' is not ready during scaling down, "+
			"please verify its state. Error: %v", scaleDownPodName, w.ObjectMeta.Name, err)
	}

	// If we are in state of recovery is needed the setup of the server has to be already done
	if scaleDownPod.Annotations[markerRecoveryPort] == "" {
		reqLogger.Info("Verification the recovery listener is setup to run transaction recovery at " + scaleDownPodName)
		// Verify the recovery listener is setup
		jsonResult, err := wfly.ExecuteMgmtOp(scaleDownPod, wfly.MgmtOpTxnCheckRecoveryListener)
		if err != nil {
			return false, "", fmt.Errorf("Cannot check if the transaction recovery listener is enabled for recovery at pod %v, error: %v", scaleDownPodName, err)
		}
		if !wfly.IsMgmtOutcomeSuccesful(jsonResult) {
			return false, "", fmt.Errorf("Failed to verify if transaction recovery listener is enabled at pod %v. Scaledown processing cannot trigger recovery. "+
				"Management command: %v, JSON response: %v", scaleDownPodName, wfly.MgmtOpTxnCheckRecoveryListener, jsonResult)
		}
		// When listener is not enabled then the pod will be terminated
		isTxRecoveryListenerEnabledAsInterface := wfly.ReadJSONDataByIndex(jsonResult, "result")
		isTxRecoveryListenerEnabledAsString, _ := wfly.ConvertToString(isTxRecoveryListenerEnabledAsInterface)
		if txrecoverydefined, err := strconv.ParseBool(isTxRecoveryListenerEnabledAsString); err == nil && !txrecoverydefined {
			reqLogger.Info("Transaction recovery listener is not enabled. Transaction recovery cannot proceed at pod " + scaleDownPodName)
			r.Recorder.Event(w, corev1.EventTypeWarning, "WildFlyServerTransactionRecovery",
				"Application server at pod "+scaleDownPodName+" does not define transaction recovery listener and recovery processing can't go forward."+
					" Please consider fixing server configuration. The pod is now going to be terminated.")
			return true, "", nil
		}

		// Reading recovery port from the app server with management port
		reqLogger.Info("Query to find the transaction recovery port to force scan at pod " + scaleDownPodName)
		queriedScaleDownPodRecoveryPort, err := wfly.GetTransactionRecoveryPort(scaleDownPod)
		if err == nil && queriedScaleDownPodRecoveryPort != 0 {
			scaleDownPodRecoveryPort = queriedScaleDownPodRecoveryPort
		}
		if err != nil {
			reqLogger.Error(err, "Error on reading transaction recovery port with management command. Using default port: "+scaleDownPodName,
				"Pod name", scaleDownPodName)
		}

		// The pod was already searched for the recovery port, marking that into the annotations
		annotations := wfly.MapMerge(
			scaleDownPod.GetAnnotations(), map[string]string{markerRecoveryPort: strconv.FormatInt(int64(scaleDownPodRecoveryPort), 10)})
		patch := client.MergeFrom(scaleDownPod.DeepCopyObject())
		scaleDownPod.SetAnnotations(annotations)
		if err := resources.Patch(w, r.Client, scaleDownPod, patch); err != nil {
			return false, "", fmt.Errorf("Failed to update pod annotations, pod name %v, annotations to be set %v, error: %v",
				scaleDownPodName, scaleDownPod.Annotations, err)
		}
	} else {
		// pod annotation already contains information on recovery port thus we just use it
		queriedScaleDownPodRecoveryPortString := scaleDownPod.Annotations[markerRecoveryPort]
		queriedScaleDownPodRecoveryPort, err := strconv.Atoi(queriedScaleDownPodRecoveryPortString)
		if err != nil {
			patch := client.MergeFrom(scaleDownPod.DeepCopyObject())
			delete(scaleDownPod.Annotations, markerRecoveryPort)
			if errUpdate := resources.Patch(w, r.Client, scaleDownPod, patch); errUpdate != nil {
				reqLogger.Info("Cannot update scaledown pod while resetting the recovery port annotation",
					"Scale down Pod", scaleDownPodName, "Annotations", scaleDownPod.Annotations, "Error", errUpdate)
			}
			return false, "", fmt.Errorf("Cannot convert recovery port value '%s' to integer for the scaling down pod %v, error: %v",
				queriedScaleDownPodRecoveryPortString, scaleDownPodName, err)
		}
		scaleDownPodRecoveryPort = int32(queriedScaleDownPodRecoveryPort)
	}

	// With enabled recovery listener and the port, let's start the recovery scan
	reqLogger.Info("Executing recovery scan at "+scaleDownPodName, "Pod IP", scaleDownPodIP, "Recovery port", scaleDownPodRecoveryPort)
	_, err = wfly.RemoteOps.SocketConnect(scaleDownPodIP, scaleDownPodRecoveryPort, txnRecoveryScanCommand)
	if err != nil {
		patch := client.MergeFrom(scaleDownPod.DeepCopyObject())
		delete(scaleDownPod.Annotations, markerRecoveryPort)
		if errUpdate := r.Client.Patch(context.TODO(), scaleDownPod, patch); errUpdate != nil {
			reqLogger.Info("Cannot update scaledown pod while resetting the recovery port annotation",
				"Scale down Pod", scaleDownPodName, "Annotations", scaleDownPod.Annotations, "Error", errUpdate)
		}
		return false, "", fmt.Errorf("Failed to run transaction recovery scan for scaling down pod %v. "+
			"Please, verify the pod log file. Error: %v", scaleDownPodName, err)
	}
	// No error on recovery scan => all the registered resources were available during the recovery processing
	foundLogLine, err := wfly.RemoteOps.VerifyLogContainsRegexp(scaleDownPod, scaleDownPodLogTimestampAtStart, recoveryErrorRegExp)
	if err != nil {
		return false, "", fmt.Errorf("Cannot parse log from scaling down pod %v, error: %v", scaleDownPodName, err)
	}
	if foundLogLine != "" {
		retString := fmt.Sprintf("Scale down transaction recovery processing contains errors in log. The recovery will be retried."+
			"Pod name: %v, log line with error '%v'", scaleDownPod, foundLogLine)
		return false, retString, nil
	}
	// Probing transaction log to verify there is not in-doubt transaction in the log
	_, err = wfly.ExecuteMgmtOp(scaleDownPod, wfly.MgmtOpTxnProbe)
	if err != nil {
		return false, "", fmt.Errorf("Error in probing transaction log for scaling down pod %v, error: %v", scaleDownPodName, err)
	}
	// Transaction log was probed, now we read the set of transactions which are in-doubt
	jsonResult, err := wfly.ExecuteMgmtOp(scaleDownPod, wfly.MgmtOpTxnRead)
	if err != nil {
		return false, "", fmt.Errorf("Cannot read transactions from the transaction log for pod scaling down %v, error: %v", scaleDownPodName, err)
	}
	if !wfly.IsMgmtOutcomeSuccesful(jsonResult) {
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
	lsCommand := fmt.Sprintf(`ls ${JBOSS_HOME}/%s/%s/ 2> /dev/null || true`, resources.StandaloneServerDataDirRelativePath, wftcDataDirName)
	commandResult, err := wfly.RemoteOps.Execute(scaleDownPod, lsCommand)
	if err != nil {
		return false, "", fmt.Errorf("Cannot query filesystem at scaling down pod %v to check existing remote transactions. "+
			"Exec command: %v", scaleDownPodName, lsCommand)
	}
	if commandResult != "" {
		retString := fmt.Sprintf("WildFly Transaction Client data dir is not empty and scaling down of the pod '%v' will be retried."+
			"Wildfly Transacton Client data dir path '${JBOSS_HOME}/%v/%v', output listing: %v",
			scaleDownPodName, resources.StandaloneServerDataDirRelativePath, wftcDataDirName, commandResult)
		return false, retString, nil
	}
	return true, "", nil
}

func (r *WildFlyServerReconciler) setupRecoveryPropertiesAndRestart(reqLogger logr.Logger, scaleDownPod *corev1.Pod, w *wildflyv1alpha1.WildFlyServer) (mustReconcile int, err error) {
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
			resources.Delete(w, r.Client, scaleDownPod)
			return requeueNow, fmt.Errorf("Cannot restart application server at pod %v after setting up the periodic recovery properties. "+
				"The pod was deleted to be restarted for new recovery attempt. Error: %v", scaleDownPodName, err)
		}

		reqLogger.Info("Marking pod as being setup for transaction recovery. Adding annotation "+markerRecoveryPropertiesSetup, "Pod Name", scaleDownPodName)
		annotations := wfly.MapMerge(
			scaleDownPod.GetAnnotations(), map[string]string{markerRecoveryPropertiesSetup: "true"})
		patch := client.MergeFrom(scaleDownPod.DeepCopyObject())
		scaleDownPod.SetAnnotations(annotations)
		if err := resources.Patch(w, r.Client, scaleDownPod, patch); err != nil {
			return requeueNow, fmt.Errorf("Failed to update pod annotations, pod name %v, annotations to be set %v, error: %v",
				scaleDownPodName, scaleDownPod.Annotations, err)
		}

		return requeueOff, nil
	}
	reqLogger.Info("Recovery properties at pod were already defined. Skipping server restart.", "Pod Name", scaleDownPodName)
	return requeueOff, nil
}

func (r *WildFlyServerReconciler) updatePodLabel(w *wildflyv1alpha1.WildFlyServer, scaleDownPod *corev1.Pod, labelName, labelValue string) (bool, error) {
	updated := false
	if scaleDownPod.ObjectMeta.Labels[labelName] != labelValue {
		patch := client.MergeFrom(scaleDownPod.DeepCopyObject())
		scaleDownPod.ObjectMeta.Labels[labelName] = labelValue
		if err := resources.Patch(w, r.Client, scaleDownPod, patch); err != nil {
			return false, fmt.Errorf("Failed to update pod labels for pod %v with label [%s=%s], error: %v",
				scaleDownPod.ObjectMeta.Name, labelName, labelValue, err)
		}
		updated = true
	}
	return updated, nil
}

// processTransactionRecoveryScaleDown runs transaction recovery on provided number of pods
//   mustReconcile returns int constant; 'requeueNow' if the reconcile requeue loop should be called as soon as possible,
//                 'requeueLater' if requeue loop is needed but it could be delayed, 'requeueOff' if requeue loop is not necessary
//   err reports error which occurs during method processing
func (r *WildFlyServerReconciler) processTransactionRecoveryScaleDown(reqLogger logr.Logger, w *wildflyv1alpha1.WildFlyServer,
	numberOfPodsToScaleDown int, podList *corev1.PodList) (mustReconcile int, err error) {

	wildflyServerNumberOfPods := len(podList.Items)
	scaleDownPodsStates := sync.Map{} // map referring to: pod name - pod state
	scaleDownErrors := sync.Map{}     // errors occurred during processing the scaledown for the pods
	mustReconcile = requeueOff

	if wildflyServerNumberOfPods == 0 || numberOfPodsToScaleDown == 0 {
		// no active pods to scale down or no pods are requested to scale down
		return requeueOff, nil
	}
	if w.Spec.BootableJar {
		reqLogger.Info("Transaction scale down recovery is unsupported for Bootable JAR pods. The pods will be removed without checking pending transactions.",
			"Number of pods to be scaled down", numberOfPodsToScaleDown)
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
			reqLogger.Info("Transaction scale down recovery will be skipped as it's unsupported when WildFlyServer Storage uses 'EmptyDir' "+
				"and transaction subsystem does not use the JDBC object store. The recovery processing is unsafe. "+
				"Please configure WildFlyServer.Spec.Storage with a Persistent Volume Claim or use database to store transaction log.",
				"Number of pods to be scaled down", numberOfPodsToScaleDown)
			return r.skipRecoveryAndForceScaleDown(w, wildflyServerNumberOfPods, numberOfPodsToScaleDown, podList)
		}
	}

	// Setting-up the pod status - status is used to decide if the pod could be scaled (aka. removed from the statefulset)
	updated := abool.New()
	for scaleDownIndex := 1; scaleDownIndex <= numberOfPodsToScaleDown; scaleDownIndex++ {
		scaleDownPodName := podList.Items[wildflyServerNumberOfPods-scaleDownIndex].ObjectMeta.Name
		wildflyServerSpecPodStatus := getWildflyServerPodStatusByName(w, scaleDownPodName)
		if wildflyServerSpecPodStatus == nil {
			continue
		}
		if wildflyServerSpecPodStatus.State == wildflyv1alpha1.PodStateActive {
			wildflyServerSpecPodStatus.State = wildflyv1alpha1.PodStateScalingDownRecoveryInvestigation
			scaleDownPodsStates.Store(scaleDownPodName, wildflyv1alpha1.PodStateScalingDownRecoveryInvestigation)
			updated.Set()
		} else {
			scaleDownPodsStates.Store(scaleDownPodName, wildflyServerSpecPodStatus.State)
		}
	}
	if updated.IsSet() { // updating status of pods as soon as possible
		w.Status.ScalingdownPods = int32(numberOfPodsToScaleDown)
		err := resources.UpdateWildFlyServerStatus(w, r.Client)
		if err != nil {
			return requeueNow, fmt.Errorf("There was trouble to update state of WildflyServer: %v, error: %v", w.Status.Pods, err)
		}
	}

	updated.UnSet()
	var wg sync.WaitGroup
	for scaleDownIndex := 1; scaleDownIndex <= numberOfPodsToScaleDown; scaleDownIndex++ {
		scaleDownPod := podList.Items[wildflyServerNumberOfPods-scaleDownIndex]
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Scaledown scenario, need to handle transaction recovery
			scaleDownPodName := scaleDownPod.ObjectMeta.Name
			scaleDownPodIP := scaleDownPod.Status.PodIP
			if strings.Contains(scaleDownPodIP, ":") && !strings.HasPrefix(scaleDownPodIP, "[") {
				scaleDownPodIP = "[" + scaleDownPodIP + "]" // for IPv6
			}

			podState, ok := scaleDownPodsStates.Load(scaleDownPodName)
			if !ok {
				scaleDownErrors.Store(scaleDownPodName+"_status-update",
					fmt.Errorf("Cannot find pod name '%v' in the list of the active pods for the WildflyServer operator: %v",
						scaleDownPodName, w.ObjectMeta.Name))
				_, podsStatus := getPodStatus(podList.Items, w.Status.Pods)
				reqLogger.Info("Updating pod status", "Pod Status", podsStatus)
				w.Status.Pods = podsStatus
				updated.Set()
				return
			}

			if podState != wildflyv1alpha1.PodStateScalingDownClean {
				reqLogger.Info("Transaction recovery scaledown processing", "Pod Name", scaleDownPodName,
					"IP Address", scaleDownPodIP, "Pod State", podState, "Pod Phase", scaleDownPod.Status.Phase)

				// For full and correct recovery we need to first, run two recovery checks and second, having the orphan detection interval set to minimum
				needsReconcile, setupErr := r.setupRecoveryPropertiesAndRestart(reqLogger, &scaleDownPod, w)
				if needsReconcile > mustReconcile {
					mustReconcile = needsReconcile
				}
				if setupErr != nil {
					scaleDownErrors.Store(scaleDownPodName, setupErr)
					return
				}
				//The futher recovery attempts won't succeed until reconcilation loop is repeated
				if needsReconcile != requeueOff {
					return
				}
				// Running recovery twice for orphan detection will be kicked-in
				_, _, recoveryErr := r.checkRecovery(reqLogger, &scaleDownPod, w)
				if recoveryErr != nil {
					scaleDownErrors.Store(scaleDownPodName, recoveryErr)
					return
				}
				success, message, recoveryErr := r.checkRecovery(reqLogger, &scaleDownPod, w)
				if recoveryErr != nil {
					scaleDownErrors.Store(scaleDownPodName, recoveryErr)
					return
				}
				if success {
					// Recovery was processed with success, the pod is clean to go
					scaleDownPodsStates.Store(scaleDownPodName, wildflyv1alpha1.PodStateScalingDownClean)
				} else if message != "" {
					// Some in-doubt transaction left in store, the pod is still dirty
					reqLogger.Info("In-doubt transactions in object store", "Pod Name", scaleDownPodName, "Message", message)
					scaleDownPodsStates.Store(scaleDownPodName, wildflyv1alpha1.PodStateScalingDownRecoveryDirty)
				}
			}
		}() // execution of the go routine for one pod
	}
	wg.Wait()

	// Updating the pod state based on the recovery processing when a scale down is in progress
	for wildflyServerPodStatusIndex, v := range w.Status.Pods {
		if podStateValue, exist := scaleDownPodsStates.Load(v.Name); exist {
			if w.Status.Pods[wildflyServerPodStatusIndex].State != podStateValue.(string) {
				updated.Set()
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
		r.Recorder.Event(w, corev1.EventTypeWarning, "WildFlyServerScaledown",
			"Errors during transaction recovery scaledown processing. Consult operator log.")
	}

	if updated.IsSet() { // recovery changed the state of the pods
		w.Status.ScalingdownPods = int32(numberOfPodsToScaleDown)
		err := resources.UpdateWildFlyServerStatus(w, r.Client)
		if err != nil {
			return requeueNow, fmt.Errorf("Error to update state of WildflyServer after recovery processing for pods %v, "+
				"error: %v. Recovery processing errors: %v", w.Status.Pods, err, resultError)
		}
	}

	return mustReconcile, resultError
}

// setLabelAsDisabled returns true when label was updated or an issue on update requires recociliation
//  returns error when an error occurs during processing, otherwise if no error occurs nil is returned
func (r *WildFlyServerReconciler) setLabelAsDisabled(w *wildflyv1alpha1.WildFlyServer, reqLogger logr.Logger, labelName string, numberOfPodsToScaleDown int,
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
func (r *WildFlyServerReconciler) skipRecoveryAndForceScaleDown(w *wildflyv1alpha1.WildFlyServer, totalNumberOfPods int,
	numberOfPodsToScaleDown int, podList *corev1.PodList) (mustReconcile int, err error) {
	log := r.Log

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
	err = resources.UpdateWildFlyServerStatus(w, r.Client)
	if err != nil {
		log.Error(err, "Error on updating WildFlyServer when skipping recovery scale down")
	}
	return requeueOff, nil
}
