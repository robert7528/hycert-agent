package deployer

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/backup"
	"github.com/hysp/hycert-agent/internal/executor"
	"github.com/hysp/hycert-agent/internal/model"
	"github.com/hysp/hycert-agent/internal/osutil"
	"github.com/hysp/hycert-agent/internal/verify"
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

	// Download cert PEM.
	certResult, err := client.DownloadCert(dep.CertificateID, api.DownloadOptions{Format: "pem"})
	if err != nil {
		return "", fmt.Errorf("download cert: %w", err)
	}

	// Download key PEM (if key_path is set).
	var keyResult *model.DownloadResult
	if detail.KeyPath != "" {
		keyResult, err = client.DownloadCert(dep.CertificateID, api.DownloadOptions{Format: "key"})
		if err != nil {
			return "", fmt.Errorf("download key: %w", err)
		}
	}

	// Parse cert once — reused for fingerprint and SNI selection.
	leafCert, err := parseLeafCert([]byte(certResult.Content))
	if err != nil {
		return "", fmt.Errorf("parse downloaded cert: %w", err)
	}
	fingerprint := fingerprintFromCert(leafCert)

	// Backup existing files (per-deployment subdirectory).
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

	// Atomic write cert + key.
	if err := writeFile(detail.CertPath, []byte(certResult.Content), 0644); err != nil {
		return "", fmt.Errorf("write cert: %w", err)
	}
	if keyResult != nil {
		if err := writeFile(detail.KeyPath, []byte(keyResult.Content), 0600); err != nil {
			return "", fmt.Errorf("write key: %w", err)
		}
	}

	// Reload service (per-service timeout).
	if detail.ReloadCmd != "" {
		reloadTO := executor.ReloadTimeoutFor(dep.TargetService, detail.ReloadTimeout)
		if out, err := executor.RunWithTimeout(ctx, detail.ReloadCmd, reloadTO); err != nil {
			return "", fmt.Errorf("reload: %w (output: %s)", err, out)
		}
	}

	// Post-deploy TLS verification. SkipVerify disables entirely; otherwise
	// connect to VerifyHost:VerifyPort (defaults: 127.0.0.1 / dep.Port or
	// 443) and assert the presented fingerprint matches.
	if detail.SkipVerify {
		// Log prominently: skipping verify gives up the main value-add of
		// this feature. Operators should see this in routine log review so
		// it doesn't silently become the default escape hatch.
		slog.Warn("post-deploy TLS verification skipped per target_detail.skip_verify",
			"deployment_id", dep.ID,
			"service", dep.TargetService,
			"host", dep.TargetHost,
			"cert_fingerprint", fingerprint,
		)
		return fingerprint, nil
	}

	probe := verify.ProbeTLS(ctx, verify.VerifyRequest{
		Host:                verifyHost(detail),
		Port:                verifyPort(detail, dep),
		SNIList:             sniList(leafCert),
		ExpectedFingerprint: fingerprint,
		Timeout:             verifyTimeoutFor(dep.TargetService, detail.VerifyTimeoutSeconds),
	})
	if probe.Result == verify.ResultMatch {
		return fingerprint, nil
	}
	return fingerprint, &VerifyError{
		Fingerprint: fingerprint,
		Result:      probe.Result,
		Detail:      probe.ErrorDetail,
		Actual:      probe.ActualFingerprints,
	}
}

// writeFile ensures the parent directory exists and atomically writes
// data with the requested permission. All deployers go through this so
// the atomic-write + correct-mode-at-creation guarantees apply
// uniformly (avoids nginx reading a half-written PEM mid-write).
func writeFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	return osutil.WriteFileAtomic(path, data, perm)
}

// verifyHost returns the TLS probe target host, defaulting to 127.0.0.1
// when target_detail does not specify one (agent usually probes itself).
func verifyHost(detail model.TargetDetail) string {
	if detail.VerifyHost != "" {
		return detail.VerifyHost
	}
	return "127.0.0.1"
}

// verifyPort picks the TLS probe port in this order:
//  1. target_detail.verify_port (explicit override)
//  2. deployment.Port (the port the service is configured to serve on)
//  3. 443 (sensible default)
func verifyPort(detail model.TargetDetail, dep model.AgentDeployment) int {
	if detail.VerifyPort > 0 {
		return detail.VerifyPort
	}
	if dep.Port != nil && *dep.Port > 0 {
		return *dep.Port
	}
	return 443
}

// sniList builds the SNI list for probing. Skeleton returns at most one
// SNI (first concrete non-wildcard from SAN/CN); later steps expand to
// multi-SNI parallel probing.
func sniList(cert *x509.Certificate) []string {
	if sni := pickSNI(cert); sni != "" {
		return []string{sni}
	}
	return nil
}

// verifyTimeoutFor picks a reasonable total probe budget per service
// type, unless the deployment overrides it explicitly.
//
// Mirrors reload timeout tiers — cold-start services need longer verify
// windows since they may still be warming up after reload returned.
func verifyTimeoutFor(service string, overrideSeconds int) time.Duration {
	if overrideSeconds > 0 {
		return time.Duration(overrideSeconds) * time.Second
	}
	switch service {
	case "tomcat", "iis":
		return 180 * time.Second
	case "kubernetes":
		return 60 * time.Second
	default:
		return 30 * time.Second
	}
}
