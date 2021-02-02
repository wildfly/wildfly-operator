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
	wildflyutil "github.com/wildfly/wildfly-operator/pkg/controller/util"
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

func (r *ReconcileWildFlyServer) checkRecovery(reqLogger logr.Logger, scaleDownPod *corev1.Pod, w *wildflyv1alpha1.WildFlyServer) (bool, string, error) {
	scaleDownPodName := scaleDownPod.ObjectMeta.Name
	scaleDownPodIP := scaleDownPod.Status.PodIP
	scaleDownPodRecoveryPort := defaultRecoveryPort

	// Reading timestamp for the latest log record
	scaleDownPodLogTimestampAtStart, err := wildflyutil.ObtainLogLatestTimestamp(scaleDownPod)
	if err != nil {
		return false, "", fmt.Errorf("Log of the pod '%s' of WildflyServer '%v' is not ready during scaling down, "+
			"please verify its state. Error: %v", scaleDownPodName, w.ObjectMeta.Name, err)
	}

	// If we are in state of recovery is needed the setup of the server has to be already done
	if scaleDownPod.Annotations[markerRecoveryPort] == "" {
		reqLogger.Info("Verification the recovery listener is setup to run transaction recovery at " + scaleDownPodName)
		// Verify the recovery listener is setup
		jsonResult, err := wildflyutil.ExecuteMgmtOp(scaleDownPod, wildflyutil.MgmtOpTxnCheckRecoveryListener)
		if err != nil {
			return false, "", fmt.Errorf("Cannot check if the transaction recovery listener is enabled for recovery at pod %v, error: %v", scaleDownPodName, err)
		}
		if !wildflyutil.IsMgmtOutcomeSuccesful(jsonResult) {
			return false, "", fmt.Errorf("Failed to verify if transaction recovery listener is enabled at pod %v. Scaledown processing cannot trigger recovery. "+
				"Management command: %v, JSON response: %v", scaleDownPodName, wildflyutil.MgmtOpTxnCheckRecoveryListener, jsonResult)
		}
		// When listener is not enabled then the pod will be terminated
		isTxRecoveryListenerEnabledAsInterface := wildflyutil.ReadJSONDataByIndex(jsonResult, "result")
		isTxRecoveryListenerEnabledAsString, _ := wildflyutil.ConvertToString(isTxRecoveryListenerEnabledAsInterface)
		if txrecoverydefined, err := strconv.ParseBool(isTxRecoveryListenerEnabledAsString); err == nil && !txrecoverydefined {
			reqLogger.Info("Transaction recovery listener is not enabled. Transaction recovery cannot proceed at pod " + scaleDownPodName)
			r.recorder.Event(w, corev1.EventTypeWarning, "WildFlyServerTransactionRecovery",
				"Application server at pod "+scaleDownPodName+" does not define transaction recovery listener and recovery processing can't go forward."+
					" Please consider fixing server configuration. The pod is now going to be terminated.")
			return true, "", nil
		}

		// Reading recovery port from the app server with management port
		reqLogger.Info("Query to find the transaction recovery port to force scan at pod " + scaleDownPodName)
		queriedScaleDownPodRecoveryPort, err := wildflyutil.GetTransactionRecoveryPort(scaleDownPod)
		if err == nil && queriedScaleDownPodRecoveryPort != 0 {
			scaleDownPodRecoveryPort = queriedScaleDownPodRecoveryPort
		}
		if err != nil {
			reqLogger.Error(err, "Error on reading transaction recovery port with management command. Using default port: "+scaleDownPodName,
				"Pod name", scaleDownPodName)
		}

		// The pod was already searched for the recovery port, marking that into the annotations
		annotations := wildflyutil.MapMerge(
			scaleDownPod.GetAnnotations(), map[string]string{markerRecoveryPort: strconv.FormatInt(int64(scaleDownPodRecoveryPort), 10)})
		scaleDownPod.SetAnnotations(annotations)
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
			if errUpdate := resources.Update(w, r.client, scaleDownPod); errUpdate != nil {
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
	_, err = wildflyutil.SocketConnect(scaleDownPodIP, scaleDownPodRecoveryPort, txnRecoveryScanCommand)
	if err != nil {
		delete(scaleDownPod.Annotations, markerRecoveryPort)
		if errUpdate := r.client.Update(context.TODO(), scaleDownPod); errUpdate != nil {
			reqLogger.Info("Cannot update scaledown pod while resetting the recovery port annotation",
				"Scale down Pod", scaleDownPodName, "Annotations", scaleDownPod.Annotations, "Error", errUpdate)
		}
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
	_, err = wildflyutil.ExecuteMgmtOp(scaleDownPod, wildflyutil.MgmtOpTxnProbe)
	if err != nil {
		return false, "", fmt.Errorf("Error in probing transaction log for scaling down pod %v, error: %v", scaleDownPodName, err)
	}
	// Transaction log was probed, now we read the set of transactions which are in-doubt
	jsonResult, err := wildflyutil.ExecuteMgmtOp(scaleDownPod, wildflyutil.MgmtOpTxnRead)
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
	lsCommand := fmt.Sprintf(`ls ${JBOSS_HOME}/%s/%s/ 2> /dev/null || true`, resources.StandaloneServerDataDirRelativePath, wftcDataDirName)
	commandResult, err := wildflyutil.ExecRemote(scaleDownPod, lsCommand)
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
	if isRunning, errCli := wildflyutil.IsAppServerRunningViaJBossCli(scaleDownPod); !isRunning {
		reqLogger.Info("Pod is labeled as running but the JBoss CLI cannot. It will be hopefully accessible in a while for "+
			"transaction recovery may proceed with scale down.", "Pod Name", scaleDownPodName, "Object Name", w.ObjectMeta.Name,
			"Error", errCli)
		return requeueLater, nil
	}

	// Setup and restart only if it was not done in the prior reconcile cycle
	if scaleDownPod.Annotations[markerRecoveryPropertiesSetup] == "" {
		reqLogger.Info("Setting up back-off period and orphan detection properties for scaledown transaction recovery", "Pod Name", scaleDownPodName)
		setPeriodOps := fmt.Sprintf(wildflyutil.MgmtOpSystemPropertyRecoveryBackoffPeriod, "1") + "," + fmt.Sprintf(wildflyutil.MgmtOpSystemPropertyOrphanSafetyInterval, "1")
		disableTMOp := fmt.Sprintf(wildflyutil.MgmtOpSystemPropertyTransactionManagerDisabled, "true")
		jsonResult, errExecution := wildflyutil.ExecuteMgmtOp(scaleDownPod, setPeriodOps+","+disableTMOp)

		// command may end-up with error with status 'rolled-back' which means duplication, thus do not check for error as error could be positive outcome
		isOperationRolledBack := wildflyutil.ReadJSONDataByIndex(jsonResult, "rolled-back")
		isOperationRolledBackAsString, _ := wildflyutil.ConvertToString(isOperationRolledBack)
		if !wildflyutil.IsMgmtOutcomeSuccesful(jsonResult) && isOperationRolledBackAsString != "true" {
			return requeueLater, fmt.Errorf("Error on setting up the back-off and orphan detection periods. "+
				"The jboss-cli.sh operation '%s' at pod %s failed. JSON output: %v, command error: %v",
				setPeriodOps, scaleDownPodName, jsonResult, errExecution)
		}

		reqLogger.Info("Restarting application server to apply the env properties", "Pod Name", scaleDownPodName)
		if err := wildflyutil.ExecuteOpAndWaitForServerBeingReady(reqLogger, wildflyutil.MgmtOpRestart, scaleDownPod); err != nil {
			reqLogger.Error(err, "Cannot restart application server after setting up the periodic recovery properties, "+
				"pod will be deleted to be relaunched.", "Pod Name", scaleDownPodName)
			resources.Delete(w, r.client, scaleDownPod)
		}

		reqLogger.Info("Marking pod as being setup for transaction recovery. Adding annotation "+markerRecoveryPropertiesSetup, "Pod Name", scaleDownPodName)
		annotations := wildflyutil.MapMerge(
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

// processTransactionRecoveryScaleDown runs transaction recovery on provided number of pods
//   mustReconcile returns int constant; 'requeueNow' if the reconcile requeue loop should be called as soon as possible,
//                 'requeueLater' if requeue loop is needed but it could be delayed, 'requeueOff' if requeue loop is not necessary
//   err reports error which occurs during method processing
func (r *ReconcileWildFlyServer) processTransactionRecoveryScaleDown(reqLogger logr.Logger, w *wildflyv1alpha1.WildFlyServer,
	numberOfPodsToScaleDown int, podList *corev1.PodList) (mustReconcile int, err error) {

	wildflyServerNumberOfPods := len(podList.Items)
	scaleDownPodsStates := sync.Map{} // map referring to: pod name - pod state
	scaleDownErrors := sync.Map{}     // errors occurred during processing the scaledown for the pods
	mustReconcile = requeueOff

	if w.Spec.BootableJar {
		for scaleDownIndex := 1; scaleDownIndex <= numberOfPodsToScaleDown; scaleDownIndex++ {
			scaleDownPodName := podList.Items[wildflyServerNumberOfPods-scaleDownIndex].ObjectMeta.Name
			wildflyServerSpecPodStatus := getWildflyServerPodStatusByName(w, scaleDownPodName)
			if wildflyServerSpecPodStatus == nil {
				continue
			}
			reqLogger.Info("Transaction recovery is unsupported for Bootable JAR pods. The " + scaleDownPodName + " pod will be removed without checking pending transactions.")
			wildflyServerSpecPodStatus.State = wildflyv1alpha1.PodStateScalingDownClean
		}
		w.Status.ScalingdownPods = int32(numberOfPodsToScaleDown)
		return requeueOff, nil
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
		err := resources.UpdateWildFlyServerStatus(w, r.client)
		if err != nil {
			err = fmt.Errorf("There was trouble to update state of WildflyServer: %v, error: %v", w.Status.Pods, err)
		}
		return requeueNow, err
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

				// TODO: check if it's possible to set graceful shutdown here

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
		r.recorder.Event(w, corev1.EventTypeWarning, "WildFlyServerScaledown",
			"Errors during transaction recovery scaledown processing. Consult operator log.")
	}

	if updated.IsSet() { // recovery changed the state of the pods
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
