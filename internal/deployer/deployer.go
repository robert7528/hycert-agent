package deployer

import (
	"context"
	"fmt"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/model"
)

// Deployer writes certificates to disk and reloads the target service.
type Deployer interface {
	Deploy(ctx context.Context, client *api.Client, dep model.AgentDeployment) (fingerprint string, err error)
}

// registry maps target_service names to Deployer instances.
var registry = map[string]func(backupEnabled bool, backupDir string) Deployer{}

// Register adds a deployer factory to the registry.
func Register(service string, factory func(backupEnabled bool, backupDir string) Deployer) {
	registry[service] = factory
}

// Get returns a Deployer for the given target_service.
func Get(service string, backupEnabled bool, backupDir string) (Deployer, error) {
	factory, ok := registry[service]
	if !ok {
		return nil, fmt.Errorf("unsupported target_service: %s", service)
	}
	return factory(backupEnabled, backupDir), nil
}

func init() {
	// PEM deployer: cert + key as separate files (nginx, apache, hyproxy)
	for _, svc := range []string{"nginx", "apache", "hyproxy"} {
		svc := svc
		_ = svc
		Register(svc, func(backupEnabled bool, backupDir string) Deployer {
			return &PEMDeployer{BackupEnabled: backupEnabled, BackupDir: backupDir}
		})
	}

	// PEM combined deployer: cert+key in single file (haproxy)
	for _, svc := range []string{"haproxy"} {
		svc := svc
		_ = svc
		Register(svc, func(backupEnabled bool, backupDir string) Deployer {
			return &PEMCombinedDeployer{BackupEnabled: backupEnabled, BackupDir: backupDir}
		})
	}

	// JKS deployer: Java keystore (tomcat)
	for _, svc := range []string{"tomcat"} {
		svc := svc
		_ = svc
		Register(svc, func(backupEnabled bool, backupDir string) Deployer {
			return &JKSDeployer{BackupEnabled: backupEnabled, BackupDir: backupDir}
		})
	}

	// PFX deployer: PKCS#12 keystore (iis)
	Register("iis", func(backupEnabled bool, backupDir string) Deployer {
		return &PFXDeployer{BackupEnabled: backupEnabled, BackupDir: backupDir}
	})

	// K8S deployer: Kubernetes TLS Secret
	Register("kubernetes", func(backupEnabled bool, backupDir string) Deployer {
		return &K8SDeployer{BackupEnabled: backupEnabled, BackupDir: backupDir}
	})
}
