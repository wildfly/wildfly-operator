package util

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
)

var (
	// number of retries when waiting for container restart/reload is done
	restartRetryCounter = GetEnvAsInt("SERVER_RESTART_RETRY_COUNTER", 10)

	// MgmtOpServerStateRead is a JBoss CLI command for reading WFLY server
	MgmtOpServerStateRead = ":read-attribute(name=server-state)"
	// MgmtOpListSubsystems is a JBoss CLI command to read names of available subsystems
	MgmtOpListSubsystems = ":read-children-names(child-type=subsystem)"
	// MgmtOpReload is a JBoss CLI command for reloading WFLY server
	MgmtOpReload = ":reload()"
	// MgmtOpRestart is a JBoss CLI command for restarting WFLY server
	MgmtOpRestart = ":shutdown(restart=true)"
	// MgmtOpTxnCheckJdbcStore is a JBoss CLI command to verify if txn log is saved in a database
	MgmtOpTxnCheckJdbcStore = "/subsystem=transactions:read-attribute(name=use-jdbc-store)"
	// MgmtOpTxnCheckRecoveryListener is a JBoss CLI command to verify if recovery listener is active
	MgmtOpTxnCheckRecoveryListener = "/subsystem=transactions:read-attribute(name=recovery-listener)"
	// MgmtOpTxnProbe is a JBoss CLI command for probing transaction log store
	MgmtOpTxnProbe = "/subsystem=transactions/log-store=log-store:probe()"
	// MgmtOpTxnRead is a JBoss CLI command for reading transaction log store
	MgmtOpTxnRead = "/subsystem=transactions/log-store=log-store:read-children-resources(child-type=transactions,recursive=true,include-runtime=true)"
	// MgmtOpTxnRecoverySocketBindingRead is a JBoss CLI command for reading name of recovery socket binding
	MgmtOpTxnRecoverySocketBindingRead = "/subsystem=transactions:read-attribute(name=socket-binding)"
	// MgmtOpSocketBindingRead is a JBoss CLI command for reading all data on the socket binding group
	MgmtOpSocketBindingRead = "/socket-binding-group=standard-sockets:read-resource(recursive=true,resolve-expressions=true,include-runtime=true)"
	// MgmtOpSystemPropertyRecoveryBackoffPeriod is a JBoss CLI command to set system property of recovery backoff period
	MgmtOpSystemPropertyRecoveryBackoffPeriod = "/system-property=com.arjuna.ats.arjuna.common.RecoveryEnvironmentBean.recoveryBackoffPeriod:add(value=%s)"
	// MgmtOpSystemPropertyPeriodicRecoveryPeriod is a JBoss CLI command to set system property of periodic recovery period
	MgmtOpSystemPropertyPeriodicRecoveryPeriod = "/system-property=com.arjuna.ats.arjuna.common.RecoveryEnvironmentBean.periodicRecoveryPeriod:add(value=%s)"
	// MgmtOpSystemPropertyOrphanSafetyInterval is a JBoss CLI command to set system property of orphan safety interval
	MgmtOpSystemPropertyOrphanSafetyInterval = "/system-property=com.arjuna.ats.jta.common.JTAEnvironmentBean.orphanSafetyInterval:add(value=%s)"
	// MgmtOpSystemPropertyTransactionManagerDisabled is a JBoss CLI command to set system property to disable/enable transaction manager
	MgmtOpSystemPropertyTransactionManagerDisabled = "/system-property=com.arjuna.ats.arjuna.common.CoordinatorEnvironmentBean.startDisabled:add(value=%s)"
)

// IsMgmtOutcomeSuccesful verifies if the management operation was succcesfull
func IsMgmtOutcomeSuccesful(jsonBody map[string]interface{}) bool {
	outcomeAsInterface := ReadJSONDataByIndex(jsonBody, "outcome")
	outcomeAsString, _ := ConvertToString(outcomeAsInterface)
	return outcomeAsString == "success"
}

// ExecuteMgmtOp executes WildFly managemnt operation represented as a string
//  the execution runs as shh remote command with jboss-cli.sh executed on the pod
//  returns the JSON as the return value from the operation
func ExecuteMgmtOp(pod *corev1.Pod, mgmtOpString string) (map[string]interface{}, error) {
	jbossCliCommand := fmt.Sprintf("${JBOSS_HOME}/bin/jboss-cli.sh --output-json -c --commands='%s'", mgmtOpString)
	resString, err := RemoteOps.Execute(pod, jbossCliCommand)
	if err != nil && resString == "" {
		return nil, fmt.Errorf("Cannot execute JBoss CLI command %s at pod %v. Cause: %v", jbossCliCommand, pod.Name, err)
	}
	// The CLI result string may contain non JSON data like warnings, removing the prefix and suffix for parsing
	startIndex := strings.Index(resString, "{")
	if startIndex <= 0 {
		startIndex = 0
	} else {
		fmt.Printf("JBoss CLI command '%s' execution on pod '%v' returned a not cleaned JSON result with content %v",
			jbossCliCommand, pod.Name, resString)
	}
	lastIndex := strings.LastIndex(resString, "}")
	if lastIndex < startIndex {
		lastIndex = len(resString)
	} else {
		lastIndex++ // index plus one to include the } character
	}
	resIoReader := ioutil.NopCloser(strings.NewReader(resString[startIndex:lastIndex]))
	defer resIoReader.Close()
	jsonBody, err := decodeJSON(&resIoReader)
	if err != nil {
		return nil, fmt.Errorf("Cannot decode JBoss CLI '%s' executed on pod %v return data '%v' to JSON. Cause: %v",
			jbossCliCommand, pod.Name, resString, err)
	}
	return jsonBody, nil
}

// decodeJSONBody takes the io.Reader (res) as expected to be representation of a JSON
//   and decodes it to the form of the JSON type "native" to golang
func decodeJSON(reader *io.ReadCloser) (map[string]interface{}, error) {
	var resJSON map[string]interface{}
	err := json.NewDecoder(*reader).Decode(&resJSON)
	if err != nil {
		return nil, fmt.Errorf("Fail to parse reader data to JSON, error: %v", err)
	}
	return resJSON, nil
}

// ReadJSONDataByIndex iterates over the JSON object to return
//   data saved at the provided index. It returns json data as interface{}.
func ReadJSONDataByIndex(json interface{}, indexes ...string) interface{} {
	jsonInProgress := json
	for _, index := range indexes {
		switch vv := jsonInProgress.(type) {
		case map[string]interface{}:
			jsonInProgress = vv[index]
		default:
			return nil
		}
	}
	return jsonInProgress
}

// GetTransactionRecoveryPort reads management to find out the recovery port
func GetTransactionRecoveryPort(pod *corev1.Pod) (int32, error) {
	jsonResult, err := ExecuteMgmtOp(pod, MgmtOpTxnRecoverySocketBindingRead)
	if err != nil {
		return 0, fmt.Errorf("Error on management operation to read transaction recovery socket binding with command %v, error: %v",
			MgmtOpTxnRecoverySocketBindingRead, err)
	}
	if !IsMgmtOutcomeSuccesful(jsonResult) {
		return 0, fmt.Errorf("Cannot read transaction recovery socket binding. The response on command '%v' was %v",
			MgmtOpTxnRecoverySocketBindingRead, jsonResult)
	}
	nameOfSocketBinding, isString := jsonResult["result"].(string)
	if !isString {
		return 0, fmt.Errorf("Cannot parse result from reading transaction recovery socket binding. The result is '%v', from command '%v' of whole JSON result: %v",
			nameOfSocketBinding, MgmtOpTxnRecoverySocketBindingRead, jsonResult)
	}

	jsonResult, err = ExecuteMgmtOp(pod, MgmtOpSocketBindingRead)
	if err != nil {
		return 0, fmt.Errorf("Error on management operation to read socket binding group with command %v, error: %v",
			MgmtOpSocketBindingRead, err)
	}
	if !IsMgmtOutcomeSuccesful(jsonResult) {
		return 0, fmt.Errorf("Cannot read information on socket binding group. The response on command '%v' was %v",
			MgmtOpSocketBindingRead, jsonResult)
	}
	offsetPortString := ReadJSONDataByIndex(jsonResult["result"], "port-offset")
	offsetPort, err := ConvertToInt(offsetPortString)
	if err != nil {
		return 0, fmt.Errorf("Cannot read port offset with the socket binding group read command '%v' with response %v. "+
			"Cannot convert value '%v' to integer.", MgmtOpSocketBindingRead, jsonResult, offsetPortString)
	}
	recoveryPortString := ReadJSONDataByIndex(jsonResult["result"], "socket-binding", nameOfSocketBinding, "bound-port")
	recoveryPort, err := ConvertToInt(recoveryPortString)
	if err != nil {
		return 0, fmt.Errorf("Cannot read txn recovery port with the socket binding group read command '%v' with response %v. "+
			"Cannot convert value '%v' to integer.", MgmtOpSocketBindingRead, jsonResult, recoveryPortString)
	}
	return int32(recoveryPort) + int32(offsetPort), nil
}

// ExecuteOpAndWaitForServerBeingReady executes WildFly management operation on the pod
//  this operation is checked to succeed and then waits for the container is ready
//  this method is assumed to be used for reload/restart operations
//  returns error if execution was not processed successfully
func ExecuteOpAndWaitForServerBeingReady(reqLogger logr.Logger, mgmtOp string, pod *corev1.Pod) error {
	podName := pod.ObjectMeta.Name

	jsonResult, err := ExecuteMgmtOp(pod, mgmtOp)
	if err != nil {
		return fmt.Errorf("Cannot run operation '%v' at application container for down pod %s, error: %v", mgmtOp, podName, err)
	}
	if !IsMgmtOutcomeSuccesful(jsonResult) {
		return fmt.Errorf("Unsuccessful management operation '%v' for pod %s. JSON output: %v",
			mgmtOp, podName, jsonResult)
	}
	for serverStateCheckCounter := 1; serverStateCheckCounter <= int(restartRetryCounter); serverStateCheckCounter++ {
		reqLogger.Info(fmt.Sprintf("Waiting for server to be reinitialized. Iteration %v/%v", serverStateCheckCounter, restartRetryCounter), "Pod Name", podName)
		isRunning, cliErr := IsAppServerRunningViaJBossCli(pod)
		err = cliErr
		if isRunning { // app server was answered as running via jboss cli, the restart has finished
			err = nil
			break
		}
	}
	if err != nil { // restart operation has not finished yet and server is not properly running
		return fmt.Errorf("Application server was not reinitialized successfully in time. Operation '%s' "+
			"at pod %v, JSON management operation result: %v, error: %v", mgmtOp, podName, jsonResult, err)
	}
	return nil
}

// IsAppServerRunningViaJBossCli runs JBoss CLI call to app server to find out if is in state 'running'
//   if 'running' then returns true, otherwise false
//   when false is returned then error contains a message with app server's state
func IsAppServerRunningViaJBossCli(pod *corev1.Pod) (bool, error) {
	jsonResult, err := ExecuteMgmtOp(pod, MgmtOpServerStateRead)
	if err == nil {
		if !IsMgmtOutcomeSuccesful(jsonResult) {
			err = fmt.Errorf("Reading application server state with CLI command '%v' executed at pod '%v' was not succesful, JSON response: %v",
				MgmtOpServerStateRead, pod.ObjectMeta.Name, jsonResult)
		}
		if jsonResult["result"] != "running" {
			err = fmt.Errorf("Application server at pod '%v' is not running but in state '%v'", pod.ObjectMeta.Name, jsonResult["result"])
		}
	}
	return err == nil, err
}

// ListSubsystems list the available subsystems in the application server at the po
func ListSubsystems(pod *corev1.Pod) ([]string, error) {
	subsystemListResult, err := ExecuteAndGetResult(pod, MgmtOpListSubsystems)
	if err != nil {
		return nil, fmt.Errorf("Failed to verify if the transaction subsystem is available. Error: %v", err)
	}
	return ConvertToArrayString(subsystemListResult)
}

// ExecuteAndGetResult executes the JBoss CLI operation provided as the string in parameter 'managementOperation'
//   It will be executed by call of 'jboss-cli.sh' at the particular pod.
//   When succesful it returns the result part of the returned json, otherwise it returns nil with error describing failure details.
func ExecuteAndGetResult(pod *corev1.Pod, managementOperation string) (interface{}, error) {
	podName := pod.ObjectMeta.Name
	jsonResult, err := ExecuteMgmtOp(pod, managementOperation)
	if err != nil {
		return nil, fmt.Errorf("Cannot execute management operation '%v' at pod %v, error: %v", managementOperation, podName, err)
	}
	if !IsMgmtOutcomeSuccesful(jsonResult) {
		return nil, fmt.Errorf("Management operation '%v' at pod '%s' was executed but the outcome was not succesful: %v",
			managementOperation, podName, jsonResult)
	}
	cliResult := ReadJSONDataByIndex(jsonResult, "result")
	if cliResult == nil {
		return nil, fmt.Errorf("Failed to get 'result' of the management operation '%v' at pod '%s'. The full JSON output was: %v",
			managementOperation, podName, jsonResult)
	} else {
		return cliResult, nil
	}
}
