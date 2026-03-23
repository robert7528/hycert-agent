package deployer

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/backup"
	"github.com/hysp/hycert-agent/internal/executor"
	"github.com/hysp/hycert-agent/internal/model"
)

// PEMDeployer handles nginx / apache style: cert and key as separate files.
type PEMDeployer struct {
	BackupEnabled bool
	BackupDir     string
}

func (d *PEMDeployer) Deploy(ctx context.Context, client *api.Client, dep model.AgentDeployment) (string, error) {
	var detail model.TargetDetail
	if err := json.Unmarshal([]byte(dep.TargetDetail), &detail); err != nil {
		return "", fmt.Errorf("parse target_detail: %w", err)
	}

	if detail.CertPath == "" {
		return "", fmt.Errorf("cert_path is required in target_detail")
	}

	// Download cert PEM
	certResult, err := client.DownloadCert(dep.CertificateID, api.DownloadOptions{Format: "pem"})
	if err != nil {
		return "", fmt.Errorf("download cert: %w", err)
	}

	// Download key PEM (if key_path is set)
	var keyResult *model.DownloadResult
	if detail.KeyPath != "" {
		keyResult, err = client.DownloadCert(dep.CertificateID, api.DownloadOptions{Format: "key"})
		if err != nil {
			return "", fmt.Errorf("download key: %w", err)
		}
	}

	// Compute fingerprint from downloaded cert
	fingerprint, err := computeFingerprint([]byte(certResult.Content))
	if err != nil {
		return "", fmt.Errorf("compute fingerprint: %w", err)
	}

	// Backup existing files (per-deployment subdirectory)
	if d.BackupEnabled {
		backupDir := filepath.Join(d.BackupDir, fmt.Sprintf("%s-%d", dep.TargetService, dep.ID))
		if _, err := backup.File(detail.CertPath, backupDir); err != nil {
			return "", fmt.Errorf("backup cert: %w", err)
		}
		if detail.KeyPath != "" {
			if _, err := backup.File(detail.KeyPath, backupDir); err != nil {
				return "", fmt.Errorf("backup key: %w", err)
			}
		}
	}

	// Write cert file
	if err := writeFile(detail.CertPath, []byte(certResult.Content), 0644); err != nil {
		return "", fmt.Errorf("write cert: %w", err)
	}

	// Write key file
	if keyResult != nil {
		if err := writeFile(detail.KeyPath, []byte(keyResult.Content), 0600); err != nil {
			return "", fmt.Errorf("write key: %w", err)
		}
	}

	// Reload service
	if detail.ReloadCmd != "" {
		if out, err := executor.Run(ctx, detail.ReloadCmd); err != nil {
			return "", fmt.Errorf("reload: %w (output: %s)", err, out)
		}
	}

	return fingerprint, nil
}

// writeFile ensures parent directory exists, writes data, and enforces permissions.
func writeFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return err
	}
	// Enforce permissions explicitly (WriteFile won't change existing file perms)
	return os.Chmod(path, perm)
}

// computeFingerprint extracts SHA-256 fingerprint from the first PEM certificate.
func computeFingerprint(pemData []byte) (string, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return "", fmt.Errorf("no PEM block found")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse certificate: %w", err)
	}

	hash := sha256.Sum256(cert.Raw)
	// Format with colons to match hycert-api (e.g. "E8:5F:5E:BD:...")
	hex := fmt.Sprintf("%X", hash)
	parts := make([]string, 0, len(hex)/2)
	for i := 0; i < len(hex); i += 2 {
		parts = append(parts, hex[i:i+2])
	}
	return strings.Join(parts, ":"), nil
}
