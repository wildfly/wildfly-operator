package util

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	reloadRetryCount = 10 // number of retries when waiting for container reload is done
)

var (
	// MgmtOpServerStateRead is a JBoss CLI command for reading WFLY server
	MgmtOpServerStateRead = ":read-attribute(name=server-state)"
	// MgmtOpReload is a JBoss CLI command for reloading WFLY server
	MgmtOpReload = ":reload()"
	// MgmtOpRestart is a JBoss CLI command for restarting WFLY server
	MgmtOpRestart = ":shutdown(restart=true)"
	// MgmtOpTxnEnableRecoveryListener is a JBoss CLI command for enabling txn recovery listener
	MgmtOpTxnEnableRecoveryListener = "/subsystem=transactions:write-attribute(name=recovery-listener, value=true)"
	// MgmtOpTxnProbe is a JBoss CLI command for probing transaction log store
	MgmtOpTxnProbe = "/subsystem=transactions/log-store=log-store:probe()"
	// MgmtOpTxnRead is a JBoss CLI command for reading transaction log store
	MgmtOpTxnRead = "/subsystem=transactions/log-store=log-store:read-children-resources(child-type=transactions,recursive=true,include-runtime=true)"
	// MgmtOpTxnRecoverySocketBindingRead is a JBoss CLI command for reading name of recovery socket binding
	MgmtOpTxnRecoverySocketBindingRead = "/subsystem=transactions:read-attribute(name=socket-binding)"
	// MgmtOpSocketBindingRecoveryPortAddress is a JBoss CLI command for reading recovery port
	MgmtOpSocketBindingRecoveryPortAddress = "/socket-binding-group=standard-sockets/socket-binding="
	// MgmtOpSocketBindingRecoveryPortRead is a JBoss CLI command for reading recovery port
	MgmtOpSocketBindingRecoveryPortRead = ":read-attribute(name=port)"
	// MgmtOpPortOffsetRead reads port of set defined for the standard-sockets binding group
	MgmtOpPortOffsetRead = "/socket-binding-group=standard-sockets:read-attribute(name=port-offset,resolve-expressions=true)"
	// MgmtOpSystemPropertyRecoveryBackoffPeriod is a JBoss CLI command to set system property of recovery backoff period
	MgmtOpSystemPropertyRecoveryBackoffPeriod = "/system-property=com.arjuna.ats.arjuna.common.RecoveryEnvironmentBean.recoveryBackoffPeriod:add(value=%s)"
	// MgmtOpSystemPropertyPeriodicRecoveryPeriod is a JBoss CLI command to set system property of periodic recovery period
	MgmtOpSystemPropertyPeriodicRecoveryPeriod = "/system-property=com.arjuna.ats.arjuna.common.RecoveryEnvironmentBean.periodicRecoveryPeriod:add(value=%s)"
	// MgmtOpSystemPropertyOrphanSafetyInterval is a JBoss CLI command to set system property of orphan safety interval
	MgmtOpSystemPropertyOrphanSafetyInterval = "/system-property=com.arjuna.ats.jta.common.JTAEnvironmentBean.orphanSafetyInterval:add(value=%s)"
)

// IsMgmtOutcomeSuccesful verifies if the management operation was succcesfull
func IsMgmtOutcomeSuccesful(jsonBody map[string]interface{}) bool {
	return jsonBody["outcome"] == "success"
}

// ExecuteMgmtOp executes WildFly managemnt operation represented as a string
//  the execution runs as shh remote command with jboss-cli.sh executed on the pod
//  returns the JSON as the return value from the operation
func ExecuteMgmtOp(pod *corev1.Pod, jbossHome string, mgmtOpString string) (map[string]interface{}, error) {
	jbossCliCommand := fmt.Sprintf("%s/bin/jboss-cli.sh --output-json -c --command='%s'", jbossHome, mgmtOpString)
	resString, err := ExecRemote(pod, jbossCliCommand)
	if err != nil {
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
//   data saved at the provided index. It returns string.
func ReadJSONDataByIndex(json interface{}, indexes ...string) string {
	jsonInProgress := json
	for _, index := range indexes {
		switch vv := jsonInProgress.(type) {
		case map[string]interface{}:
			jsonInProgress = vv[index]
		default:
			return ""
		}
	}
	switch vv := jsonInProgress.(type) {
	case string:
		return vv
	case int:
		return strconv.Itoa(vv)
	case bool:
		return strconv.FormatBool(vv)
	default:
		return ""
	}
}

// GetTransactionRecoveryPort reads management to find out the recovery port
func GetTransactionRecoveryPort(pod *corev1.Pod, jbossHome string) (int32, error) {
	jsonResult, err := ExecuteMgmtOp(pod, jbossHome, MgmtOpTxnRecoverySocketBindingRead)
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
		return 0, fmt.Errorf("Cannot parse result from reading transaction recoery socket binding. The result is '%v', from command '%v' of whole JSON result: %v",
			nameOfSocketBinding, MgmtOpTxnRecoverySocketBindingRead, jsonResult)
	}

	mgmtOpRecoveryPortRead := MgmtOpSocketBindingRecoveryPortAddress + nameOfSocketBinding + MgmtOpSocketBindingRecoveryPortRead
	jsonResult, err = ExecuteMgmtOp(pod, jbossHome, mgmtOpRecoveryPortRead)
	if err != nil {
		return 0, fmt.Errorf("Error on management operation to read recovery port with command %v, error: %v",
			mgmtOpRecoveryPortRead, err)
	}
	if !IsMgmtOutcomeSuccesful(jsonResult) {
		return 0, fmt.Errorf("Cannot read recovery port. The response on command '%v' was %v",
			mgmtOpRecoveryPortRead, jsonResult)
	}
	recoveryPort, err := convertJSONResultToIntgo(jsonResult["result"])
	if err != nil {
		return 0, fmt.Errorf("Cannot parse result for reading recovery port. Command: '%v', JSON result: %v",
			mgmtOpRecoveryPortRead, jsonResult)
	}

	jsonResult, err = ExecuteMgmtOp(pod, jbossHome, MgmtOpPortOffsetRead)
	if err != nil {
		return 0, fmt.Errorf("Error on management operation to read port offset for socket binding with command %v, error: %v",
			MgmtOpPortOffsetRead, err)
	}
	if !IsMgmtOutcomeSuccesful(jsonResult) {
		return 0, fmt.Errorf("Cannot read port offset for socket binding. The response on command '%v' was %v",
			MgmtOpPortOffsetRead, jsonResult)
	}
	portOffset, err := convertJSONResultToIntgo(jsonResult["result"])
	if err != nil {
		return 0, fmt.Errorf("Cannot parse result for reading port offset. Command: '%v', JSON result: %v",
			mgmtOpRecoveryPortRead, jsonResult)
	}

	return recoveryPort + portOffset, nil
}

func convertJSONResultToIntgo(jsonResult interface{}) (int32, error) {
	switch v := jsonResult.(type) {
	case int32:
		return v, nil
	case int:
		return int32(v), nil
	case float64:
		return int32(int(v)), nil
	case float32:
		return int32(v), nil
	case string:
		i, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return 0, err
		}
		return int32(i), nil
	case nil:
		return 0, fmt.Errorf("The passed value is nil and cannot be converted to int32")
	default:
		return 0, fmt.Errorf("Un-expected type of passed value %v, actual type is %T", jsonResult, jsonResult)
	}
}

// ExecuteOpAndWaitForServerBeingReady executes WildFly management operation on the pod
//  this operation is checked to succeed and then waits for the container is ready
//  this is expected to be used for reload/restart operations
func ExecuteOpAndWaitForServerBeingReady(mgmtOp string, pod *corev1.Pod, jbossHome string) (bool, error) {
	podName := pod.ObjectMeta.Name

	jsonResult, err := ExecuteMgmtOp(pod, jbossHome, mgmtOp)
	if err != nil {
		return false, fmt.Errorf("Cannot run operation '%v' at application container for down pod %s, error: %v", mgmtOp, podName, err)
	}
	if !IsMgmtOutcomeSuccesful(jsonResult) {
		return false, fmt.Errorf("Not succefully running management operation '%v' for pod %s. JSON output: %v",
			mgmtOp, podName, jsonResult)
	}
	for serverStateCheckCounter := 0; err != nil && serverStateCheckCounter < reloadRetryCount; serverStateCheckCounter++ {
		jsonResult, err = ExecuteMgmtOp(pod, jbossHome, MgmtOpServerStateRead)
	}
	if err != nil { // restart operation has not finished yet and server is not properly running
		return false, fmt.Errorf("Failed waiting for server to be ready to running after operation '%s' "+
			"on pod %v, JSON management operation result: %v, error: %v", mgmtOp, podName, jsonResult, err)
	}
	return true, nil
}
