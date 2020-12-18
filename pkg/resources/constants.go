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
	// MarkerImageType is an annotation that tell us if the pod is a bootable-jar image or an generic one
	MarkerImageType = "wildfly.org/server-type"
	// ImageTypeGeneric is one of the possible values for MarkerImageType annotation
	ImageTypeGeneric = "generic"
	// ImageTypeGeneric is one of the possible values for MarkerImageType annotation denoting a bootable JAR type image
	ImageTypeBootable = "bootable-jar"
	// SecretsDir is the the directory to mount volumes from Secrets
	SecretsDir = "/etc/secrets/"
	// ConfigMapsDir is the the directory to mount volumes from ConfigMaps
	ConfigMapsDir = "/etc/configmaps/"
)

var (
	// StandaloneServerDataDirRelativePath is the path to the server standalone data directory
	StandaloneServerDataDirRelativePath = "standalone/data"
)

// JBossHome is read from the env var JBOSS_HOME or, in case of a Bootable JAR, from JBOSS_BOOTABLE_HOME
func JBossHome(bootable bool) string {
	if bootable {
		return  os.Getenv("JBOSS_BOOTABLE_HOME")
	}
	return os.Getenv("JBOSS_HOME")
}

// JBossHome is read from the env var JBOSS_HOME or, in case of a Bootable JAR, from JBOSS_BOOTABLE_DATA_DIR
func JBossHomeDataDir(bootable bool) string {
	if bootable {
		return  os.Getenv("JBOSS_BOOTABLE_DATA_DIR")
	}
	return os.Getenv("JBOSS_HOME")
}
