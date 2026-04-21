package deployer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/executor"
	"github.com/hysp/hycert-agent/internal/model"
	"github.com/hysp/hycert-agent/internal/verify"
)

// K8STargetDetail extends TargetDetail with K8S-specific fields.
type K8STargetDetail struct {
	SecretName string `json:"secret_name"`
	Namespace  string `json:"namespace"`
	Kubeconfig string `json:"kubeconfig"`
	ReloadCmd  string `json:"reload_cmd"` // optional post-deploy command

	// Post-deploy verification uses an A+B strategy:
	//   A = kubectl get secret — reads the Secret back and checks the stored
	//       cert fingerprint matches what we just applied. Cheap (10ms) and
	//       catches admission-controller mutation or apply failing silently.
	//   B = TLS probe against VerifyEndpoint with VerifySNI — the ground
	//       truth that Ingress controller (Traefik / nginx-ingress) has
	//       picked up the new Secret and is actually serving the new cert.
	//
	// VerifyEndpoint empty → skip B (log warn). This is intentional because
	// agent may not have network reach to the Ingress endpoint (multi-
	// cluster, isolated admin node). A alone is better than nothing.
	//
	// VerifySNI is NOT auto-derived from the cert because K8S Ingress
	// topology is too varied (which hostname routes to which Secret) —
	// guessing wrong leads to probe hitting the cluster default cert
	// instead of ours, surfacing as mysterious Mismatch. Let the user
	// specify the SNI explicitly in target_detail.
	VerifyEndpoint       string `json:"verify_endpoint,omitempty"` // e.g. "172.30.1.135:443"
	VerifySNI            string `json:"verify_sni,omitempty"`      // e.g. "probe.hycert.local"
	SkipVerify           bool   `json:"skip_verify,omitempty"`
	VerifyTimeoutSeconds int    `json:"verify_timeout,omitempty"`
}

// Normalize trims whitespace from string fields — see model.TargetDetail.Normalize
// for rationale.
func (d *K8STargetDetail) Normalize() {
	d.SecretName = strings.TrimSpace(d.SecretName)
	d.Namespace = strings.TrimSpace(d.Namespace)
	d.Kubeconfig = strings.TrimSpace(d.Kubeconfig)
	d.ReloadCmd = strings.TrimSpace(d.ReloadCmd)
	d.VerifyEndpoint = strings.TrimSpace(d.VerifyEndpoint)
	d.VerifySNI = strings.TrimSpace(d.VerifySNI)
}

// K8SDeployer handles Kubernetes TLS Secret updates via kubectl.
type K8SDeployer struct {
	BackupEnabled bool
	BackupDir     string
}

func (d *K8SDeployer) Deploy(ctx context.Context, client *api.Client, dep model.AgentDeployment) (string, error) {
	var detail K8STargetDetail
	if err := json.Unmarshal([]byte(dep.TargetDetail), &detail); err != nil {
		return "", fmt.Errorf("parse target_detail: %w", err)
	}
	detail.Normalize()

	if detail.SecretName == "" {
		return "", fmt.Errorf("secret_name is required in target_detail")
	}
	if detail.Namespace == "" {
		detail.Namespace = "default"
	}

	// Download cert PEM
	certResult, err := client.DownloadCert(dep.CertificateID, api.DownloadOptions{Format: "pem"})
	if err != nil {
		return "", fmt.Errorf("download cert: %w", err)
	}

	// Download key PEM
	keyResult, err := client.DownloadCert(dep.CertificateID, api.DownloadOptions{Format: "key"})
	if err != nil {
		return "", fmt.Errorf("download key: %w", err)
	}

	// Parse cert once — reused for fingerprint.
	leafCert, err := parseLeafCert([]byte(certResult.Content))
	if err != nil {
		return "", fmt.Errorf("parse downloaded cert: %w", err)
	}
	fingerprint := fingerprintFromCert(leafCert)

	// Write temp files for kubectl (cert + key)
	tmpDir, err := os.MkdirTemp("", "hycert-k8s-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	certFile := filepath.Join(tmpDir, "tls.crt")
	keyFile := filepath.Join(tmpDir, "tls.key")

	if err := os.WriteFile(certFile, []byte(certResult.Content), 0600); err != nil {
		return "", fmt.Errorf("write temp cert: %w", err)
	}
	if err := os.WriteFile(keyFile, []byte(keyResult.Content), 0600); err != nil {
		return "", fmt.Errorf("write temp key: %w", err)
	}

	// Backup existing Secret before updating
	if d.BackupEnabled {
		backupDir := filepath.Join(d.BackupDir, fmt.Sprintf("%s-%d", dep.TargetService, dep.ID))
		os.MkdirAll(backupDir, 0755)

		exportCmd := fmt.Sprintf("kubectl get secret %s -n %s -o yaml", detail.SecretName, detail.Namespace)
		exportCmd += kubeconfigFlag(detail.Kubeconfig)
		if out, err := executor.Run(ctx, exportCmd); err == nil {
			backupFile := filepath.Join(backupDir, fmt.Sprintf("%s.%s.yaml", detail.SecretName, time.Now().Format("20060102-150405")))
			os.WriteFile(backupFile, []byte(out), 0600)
		}
		// Backup failure is non-fatal — secret may not exist yet
	}

	// Build kubectl command:
	// kubectl create secret tls {name} --cert=tls.crt --key=tls.key -n {namespace} --dry-run=client -o yaml | kubectl apply -f -
	kubectlArgs := fmt.Sprintf("kubectl create secret tls %s --cert=%s --key=%s -n %s --dry-run=client -o yaml",
		detail.SecretName, certFile, keyFile, detail.Namespace)
	applyArgs := "kubectl apply -f -"

	kcFlag := kubeconfigFlag(detail.Kubeconfig)
	kubectlArgs += kcFlag
	applyArgs += kcFlag

	cmd := kubectlArgs + " | " + applyArgs

	out, err := executor.Run(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("kubectl: %w (output: %s)", err, out)
	}

	// Optional post-deploy command (e.g., rollout restart)
	if detail.ReloadCmd != "" {
		if out, err := executor.Run(ctx, detail.ReloadCmd); err != nil {
			return "", fmt.Errorf("reload: %w (output: %s)", err, out)
		}
	}

	// ── Post-deploy verification ─────────────────────────────────────────
	if detail.SkipVerify {
		slog.Warn("post-deploy verification skipped per target_detail.skip_verify",
			"deployment_id", dep.ID,
			"namespace", detail.Namespace,
			"secret", detail.SecretName,
			"cert_fingerprint", fingerprint,
		)
		return fingerprint, nil
	}

	// A: kubectl pre-check — read Secret back and verify stored cert
	// matches what we applied. Cheap sanity check: catches admission-
	// controller mutation, wrong key in data, or apply-succeeded-but-
	// data-diverged edge cases. If A fails, we don't bother with B.
	if verr := verifyK8SSecret(ctx, detail, fingerprint); verr != nil {
		return fingerprint, verr
	}

	// B: TLS probe — ground truth that the Ingress controller actually
	// serves the new cert. Optional: if VerifyEndpoint is empty we only
	// ran A, which doesn't confirm "clients see the new cert" (Ingress
	// reload could have failed silently). Log warn so it shows up in
	// routine log review.
	if detail.VerifyEndpoint == "" {
		slog.Warn("K8S verify_endpoint not configured, skipping Ingress TLS probe — only Secret content verified",
			"deployment_id", dep.ID,
			"namespace", detail.Namespace,
			"secret", detail.SecretName,
		)
		return fingerprint, nil
	}

	host, port, err := parseEndpoint(detail.VerifyEndpoint)
	if err != nil {
		return fingerprint, fmt.Errorf("invalid verify_endpoint %q: %w", detail.VerifyEndpoint, err)
	}

	sni := []string{detail.VerifySNI}
	if detail.VerifySNI == "" {
		// Fall back to host — useful when the endpoint itself is a hostname
		// (not an IP), e.g. "ingress.example.com:443".
		sni = []string{host}
	}

	probe := verify.ProbeTLS(ctx, verify.VerifyRequest{
		Host:                host,
		Port:                port,
		SNIList:             sni,
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

// verifyK8SSecret reads the Secret back via kubectl and confirms the
// stored tls.crt fingerprint matches what we applied. Returns nil on
// match, *VerifyError with ResultMismatch otherwise.
//
// Why as a separate step instead of trusting kubectl apply's exit code:
// admission/mutating webhooks can silently rewrite Secret data, or the
// Secret can land in an unexpected key. This is rare but cheap to
// verify, and the log message pinpoints "Secret content diverged" vs
// "Ingress didn't reload", which take different diagnostic paths.
func verifyK8SSecret(ctx context.Context, detail K8STargetDetail, expectedFingerprint string) *VerifyError {
	cmd := fmt.Sprintf("kubectl get secret %s -n %s -o jsonpath='{.data.tls\\.crt}'",
		detail.SecretName, detail.Namespace)
	cmd += kubeconfigFlag(detail.Kubeconfig)

	out, err := executor.Run(ctx, cmd)
	if err != nil {
		return &VerifyError{
			Fingerprint: expectedFingerprint,
			Result:      verify.ResultMismatch,
			Detail:      fmt.Sprintf("kubectl get secret failed after apply: %v", err),
		}
	}

	out = strings.TrimSpace(out)
	if out == "" {
		return &VerifyError{
			Fingerprint: expectedFingerprint,
			Result:      verify.ResultMismatch,
			Detail:      fmt.Sprintf("Secret %s/%s has no tls.crt data after apply", detail.Namespace, detail.SecretName),
		}
	}

	pemBytes, err := base64.StdEncoding.DecodeString(out)
	if err != nil {
		return &VerifyError{
			Fingerprint: expectedFingerprint,
			Result:      verify.ResultMismatch,
			Detail:      fmt.Sprintf("decode Secret tls.crt base64: %v", err),
		}
	}

	actualLeaf, err := parseLeafCert(pemBytes)
	if err != nil {
		return &VerifyError{
			Fingerprint: expectedFingerprint,
			Result:      verify.ResultMismatch,
			Detail:      fmt.Sprintf("parse Secret tls.crt: %v", err),
		}
	}

	actualFP := fingerprintFromCert(actualLeaf)
	if actualFP != expectedFingerprint {
		return &VerifyError{
			Fingerprint: expectedFingerprint,
			Result:      verify.ResultMismatch,
			Detail:      "Secret tls.crt fingerprint differs from applied cert (admission controller mutation?)",
			Actual:      map[string]string{"secret": actualFP},
		}
	}

	return nil
}

// parseEndpoint splits "host:port" into host + int port.
// Accepts hostnames, IPv4 literals, and bracketed IPv6 ([::1]:443).
func parseEndpoint(ep string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(ep)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("port %q: %w", portStr, err)
	}
	if port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("port %d out of range", port)
	}
	return host, port, nil
}

func kubeconfigFlag(kc string) string {
	if kc == "" {
		return ""
	}
	return fmt.Sprintf(" --kubeconfig=%s", kc)
}
