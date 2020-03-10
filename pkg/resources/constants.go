package resources

import (
	"os"
)

const (
	// HTTPApplicationPort is the HTTP port for the application domain
	HTTPApplicationPort int32 = 8080
	// HTTPManagementPort is the HTTP port for the management domain
	HTTPManagementPort int32 = 9990
	// MarkerServiceActive marks a pod as actively served by its service
	MarkerServiceActive = "active"
	// MarkerOperatedByLoadbalancer is a label used to remove a pod from receiving load from loadbalancer during transaction recovery
	MarkerOperatedByLoadbalancer = "wildfly.org/operated-by-loadbalancer"
	// MarkerOperatedByHeadless is a label used to remove a pod from receiving load from headless service when it's cleaned to shutdown
	MarkerOperatedByHeadless = "wildfly.org/operated-by-headless"
	// SecretsDir is the the directory to mount volumes from Secrets
	SecretsDir = "/etc/secrets/"
	// ConfigMapsDir is the the directory to mount volumes from ConfigMaps
	ConfigMapsDir = "/etc/configmaps/"
	// Mode bits to use on created config maps files
	ConfigMapFileDefaultMode int32 = 0755
)

var (
	// JBossHome is read from the env var JBOSS_HOME
	JBossHome = os.Getenv("JBOSS_HOME")
	// StandaloneServerDataDirRelativePath is the path to the server standalone data directory
	StandaloneServerDataDirRelativePath = "standalone/data"
)
