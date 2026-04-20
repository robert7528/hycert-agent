package deployer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/backup"
	"github.com/hysp/hycert-agent/internal/executor"
	"github.com/hysp/hycert-agent/internal/model"
	"github.com/hysp/hycert-agent/internal/verify"
)

// JKSDeployer handles Tomcat / Java keystore deployments.
// Downloads JKS format directly from hycert-api, writes to cert_path,
// reloads the service, then probes the TLS port to confirm the service
// is actually presenting the new cert (ground-truth verification).
type JKSDeployer struct {
	BackupEnabled bool
	BackupDir     string
}

func (d *JKSDeployer) Deploy(ctx context.Context, client *api.Client, dep model.AgentDeployment) (string, error) {
	var detail model.TargetDetail
	if err := json.Unmarshal([]byte(dep.TargetDetail), &detail); err != nil {
		return "", fmt.Errorf("parse target_detail: %w", err)
	}
	detail.Normalize()

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

	jksData, err := base64.StdEncoding.DecodeString(result.ContentBase64)
	if err != nil {
		return "", fmt.Errorf("decode jks base64: %w", err)
	}

	// Download PEM separately so we can parse the leaf certificate for
	// fingerprint computation and SNI selection. We can't parse JKS here
	// without pulling in keystore-go + password handling, and the PEM
	// download is cheap.
	certResult, err := client.DownloadCert(dep.CertificateID, api.DownloadOptions{Format: "pem"})
	if err != nil {
		return "", fmt.Errorf("download cert for fingerprint: %w", err)
	}
	leafCert, err := parseLeafCert([]byte(certResult.Content))
	if err != nil {
		return "", fmt.Errorf("parse downloaded cert: %w", err)
	}
	fingerprint := fingerprintFromCert(leafCert)

	// Backup existing keystore (per-deployment subdirectory)
	if d.BackupEnabled {
		backupDir := filepath.Join(d.BackupDir, fmt.Sprintf("%s-%d", dep.TargetService, dep.ID))
		if _, err := backup.File(detail.CertPath, backupDir); err != nil {
			return "", fmt.Errorf("backup keystore: %w", err)
		}
	}

	// Atomic write of the JKS keystore. 0600 because the keystore file
	// contains the private key (wrapped by the keystore password).
	if err := writeFile(detail.CertPath, jksData, 0600); err != nil {
		return "", fmt.Errorf("write jks: %w", err)
	}

	// Reload service. Tomcat's Restart-Service / systemctl restart tomcat
	// blocks until the Windows SCM / systemd reports Running. We give it
	// 60s by default (tier for tomcat); the deployment can override via
	// target_detail.reload_timeout. Full JVM warm-up / cert re-load is
	// covered by verify timeout, not reload timeout.
	if detail.ReloadCmd != "" {
		reloadTO := executor.ReloadTimeoutFor(dep.TargetService, detail.ReloadTimeout)
		if out, err := executor.RunWithTimeout(ctx, detail.ReloadCmd, reloadTO); err != nil {
			return "", fmt.Errorf("reload: %w (output: %s)", err, out)
		}
	}

	// Post-deploy TLS verification. Tomcat cold start is slow (JVM class
	// loading), so verifyTimeoutFor("tomcat") defaults to 180s. ProbeTLS
	// retries every RetryInterval (2s by default) until it sees the new
	// fingerprint or the budget expires.
	if detail.SkipVerify {
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
