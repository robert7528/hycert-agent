package deployer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/backup"
	"github.com/hysp/hycert-agent/internal/executor"
	"github.com/hysp/hycert-agent/internal/model"
)

// JKSDeployer handles Tomcat / Java keystore deployments.
// Downloads JKS format directly from hycert-api, writes to cert_path.
type JKSDeployer struct {
	BackupEnabled bool
	BackupDir     string
}

func (d *JKSDeployer) Deploy(ctx context.Context, client *api.Client, dep model.AgentDeployment) (string, error) {
	var detail model.TargetDetail
	if err := json.Unmarshal([]byte(dep.TargetDetail), &detail); err != nil {
		return "", fmt.Errorf("parse target_detail: %w", err)
	}

	if detail.CertPath == "" {
		return "", fmt.Errorf("cert_path is required in target_detail")
	}
	if detail.Password == "" {
		return "", fmt.Errorf("password is required in target_detail for JKS")
	}

	alias := detail.Alias
	if alias == "" {
		alias = "1"
	}

	// Download JKS format (returns base64-encoded binary)
	result, err := client.DownloadCert(dep.CertificateID, api.DownloadOptions{
		Format:   "jks",
		Password: detail.Password,
		Alias:    alias,
	})
	if err != nil {
		return "", fmt.Errorf("download jks: %w", err)
	}

	// Decode base64 content
	jksData, err := base64.StdEncoding.DecodeString(result.ContentBase64)
	if err != nil {
		return "", fmt.Errorf("decode jks base64: %w", err)
	}

	// We need to compute fingerprint from the cert — download PEM just for fingerprint
	certResult, err := client.DownloadCert(dep.CertificateID, api.DownloadOptions{Format: "pem"})
	if err != nil {
		return "", fmt.Errorf("download cert for fingerprint: %w", err)
	}
	fingerprint, err := computeFingerprint([]byte(certResult.Content))
	if err != nil {
		return "", fmt.Errorf("compute fingerprint: %w", err)
	}

	// Backup existing keystore
	if d.BackupEnabled {
		backupDir := filepath.Join(d.BackupDir, fmt.Sprintf("%s-%d", dep.TargetService, dep.ID))
		if _, err := backup.File(detail.CertPath, backupDir); err != nil {
			return "", fmt.Errorf("backup keystore: %w", err)
		}
	}

	// Write JKS file
	if err := writeFile(detail.CertPath, jksData, 0600); err != nil {
		return "", fmt.Errorf("write jks: %w", err)
	}

	// Reload service
	if detail.ReloadCmd != "" {
		if out, err := executor.Run(ctx, detail.ReloadCmd); err != nil {
			return "", fmt.Errorf("reload: %w (output: %s)", err, out)
		}
	}

	return fingerprint, nil
}
