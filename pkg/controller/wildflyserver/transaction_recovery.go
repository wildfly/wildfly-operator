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

	// Pod is not started yet. For being safely marked as clean by recovery process it has to be fully operative
	if scaleDownPod.Status.Phase == corev1.PodPending {
		return false, "", fmt.Errorf("Pod '%s' / '%v' is in pending phase %v. It will be hopefully started in a while. "+
			"Transaction recovery needs the pod being fully started to be capable to mark it as clean for the scale down.",
			scaleDownPodName, w.ObjectMeta.Name, scaleDownPod.Status.Phase)
	}

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
		jsonResult, err := wildflyutil.ExecuteMgmtOp(scaleDownPod, resources.JBossHome, wildflyutil.MgmtOpTxnCheckRecoveryListener)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "cannot execute") {
				reqLogger.Error(err, "Verify if operator JBOSS_HOME variable determines the place where the application server is installed",
					w.Name+".JBOSS_HOME", resources.JBossHome)
			}
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
		queriedScaleDownPodRecoveryPort, err := wildflyutil.GetTransactionRecoveryPort(scaleDownPod, resources.JBossHome)
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
		delete(scaleDownPod.Annotations, markerRecoveryPort)
		if err := r.client.Update(context.TODO(), scaleDownPod); err != nil {
			reqLogger.Info("Cannot update scaledown pod %v while resetting the annotation map to %v", scaleDownPodName, scaleDownPod.Annotations)
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
	_, err = wildflyutil.ExecuteMgmtOp(scaleDownPod, resources.JBossHome, wildflyutil.MgmtOpTxnProbe)
	if err != nil {
		return false, "", fmt.Errorf("Error in probing transaction log for scaling down pod %v, error: %v", scaleDownPodName, err)
	}
	// Transaction log was probed, now we read the set of transactions which are in-doubt
	jsonResult, err := wildflyutil.ExecuteMgmtOp(scaleDownPod, resources.JBossHome, wildflyutil.MgmtOpTxnRead)
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
	lsCommand := fmt.Sprintf(`ls %s/%s/ 2> /dev/null || true`, resources.StandaloneServerDataDirPath, wftcDataDirName)
	commandResult, err := wildflyutil.ExecRemote(scaleDownPod, lsCommand)
	if err != nil {
		return false, "", fmt.Errorf("Cannot query filesystem at scaling down pod %v to check existing remote transactions. "+
			"Exec command: %v", scaleDownPodName, lsCommand)
	}
	if commandResult != "" {
		retString := fmt.Sprintf("WildFly Transaction Client data dir is not empty and scaling down of the pod '%v' will be retried."+
			"Wildfly Transacton Client data dir path '%v', output listing: %v",
			scaleDownPodName, resources.StandaloneServerDataDirPath+"/"+wftcDataDirName, commandResult)
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
		setRecoveryPeriodOp := fmt.Sprintf(wildflyutil.MgmtOpSystemPropertyPeriodicRecoveryPeriod, "30") // speed-up but still having time proceed the SCAN
		wildflyutil.ExecuteMgmtOp(scaleDownPod, resources.JBossHome, setBackoffPeriodOp)
		wildflyutil.ExecuteMgmtOp(scaleDownPod, resources.JBossHome, setOrphanIntervalOp)
		jsonResult, err := wildflyutil.ExecuteMgmtOp(scaleDownPod, resources.JBossHome, setRecoveryPeriodOp)
		if err != nil {
			return false, fmt.Errorf("Error to setup recovery periods by system properties with management operation '%s' for scaling down pod %s, error: %v",
				setRecoveryPeriodOp, scaleDownPodName, err)
		}
		if !wildflyutil.IsMgmtOutcomeSuccesful(jsonResult) {
			return false, fmt.Errorf("Setting up the recovery period by system property with operation '%s' at pod %s was not succesful. JSON output: %v",
				setRecoveryPeriodOp, scaleDownPodName, jsonResult)
		}

		if _, err := wildflyutil.ExecuteOpAndWaitForServerBeingReady(wildflyutil.MgmtOpRestart, scaleDownPod, resources.JBossHome); err != nil {
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

// processTransactionRecoveryScaleDown runs transaction recovery on provided number of pods
//   mustReconcileRequeue returns true if the reconcile requeue loop should be called as soon as possible
//   err reports error which occurs during method processing
func (r *ReconcileWildFlyServer) processTransactionRecoveryScaleDown(reqLogger logr.Logger, w *wildflyv1alpha1.WildFlyServer,
	numberOfPodsToScaleDown int, podList *corev1.PodList) (mustReconcileRequeue bool, err error) {

	wildflyServerNumberOfPods := len(podList.Items)
	scaleDownPodsStates := sync.Map{} // map referring to: pod name - pod state
	scaleDownErrors := sync.Map{}     // errors occured during processing the scaledown for the pods

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
		return false, err
	}

	updated.UnSet()
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
			return true, fmt.Errorf("Error to update state of WildflyServer after recovery processing for pods %v, "+
				"error: %v. Recovery processing errors: %v", w.Status.Pods, err, resultError)
		}
	}

	return false, resultError
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
		if wildflyServerNumberOfPods-scaleDownIndex >= len(podList.Items) || wildflyServerNumberOfPods-scaleDownIndex < 0 {
			return false, fmt.Errorf("Cannot update pod label for pod number %v as there is no such active pod in list: %v, ",
				wildflyServerNumberOfPods-scaleDownIndex, podList)
		}
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
