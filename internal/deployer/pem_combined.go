package deployer

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/backup"
	"github.com/hysp/hycert-agent/internal/executor"
	"github.com/hysp/hycert-agent/internal/model"
)

// PEMCombinedDeployer handles haproxy / hyproxy style: cert+key combined in a single PEM file.
type PEMCombinedDeployer struct {
	BackupEnabled bool
	BackupDir     string
}

func (d *PEMCombinedDeployer) Deploy(ctx context.Context, client *api.Client, dep model.AgentDeployment) (string, error) {
	var detail model.TargetDetail
	if err := json.Unmarshal([]byte(dep.TargetDetail), &detail); err != nil {
		return "", fmt.Errorf("parse target_detail: %w", err)
	}

	if detail.CertPath == "" {
		return "", fmt.Errorf("cert_path is required in target_detail")
	}

	// Download combined PEM (cert + key in one response)
	result, err := client.DownloadCert(dep.CertificateID, api.DownloadOptions{
		Format:     "pem",
		IncludeKey: true,
	})
	if err != nil {
		return "", fmt.Errorf("download combined pem: %w", err)
	}

	// Compute fingerprint from downloaded cert
	fingerprint, err := computeFingerprint([]byte(result.Content))
	if err != nil {
		return "", fmt.Errorf("compute fingerprint: %w", err)
	}

	// Backup existing file (per-deployment subdirectory)
	if d.BackupEnabled {
		backupDir := filepath.Join(d.BackupDir, fmt.Sprintf("deploy-%d", dep.ID))
		if _, err := backup.File(detail.CertPath, backupDir); err != nil {
			return "", fmt.Errorf("backup cert: %w", err)
		}
	}

	// Write combined file (key included, so 0600)
	if err := writeFile(detail.CertPath, []byte(result.Content), 0600); err != nil {
		return "", fmt.Errorf("write combined pem: %w", err)
	}

	// Reload service
	if detail.ReloadCmd != "" {
		if out, err := executor.Run(ctx, detail.ReloadCmd); err != nil {
			return "", fmt.Errorf("reload: %w (output: %s)", err, out)
		}
	}

	return fingerprint, nil
}
