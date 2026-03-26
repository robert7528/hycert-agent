package deployer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/executor"
	"github.com/hysp/hycert-agent/internal/model"
)

// K8STargetDetail extends TargetDetail with K8S-specific fields.
type K8STargetDetail struct {
	SecretName string `json:"secret_name"`
	Namespace  string `json:"namespace"`
	Kubeconfig string `json:"kubeconfig"`
	ReloadCmd  string `json:"reload_cmd"` // optional post-deploy command
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

	// Compute fingerprint
	fingerprint, err := computeFingerprint([]byte(certResult.Content))
	if err != nil {
		return "", fmt.Errorf("compute fingerprint: %w", err)
	}

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

	// Build kubectl command:
	// kubectl create secret tls {name} --cert=tls.crt --key=tls.key -n {namespace} --dry-run=client -o yaml | kubectl apply -f -
	kubectlArgs := fmt.Sprintf("kubectl create secret tls %s --cert=%s --key=%s -n %s --dry-run=client -o yaml",
		detail.SecretName, certFile, keyFile, detail.Namespace)

	applyArgs := "kubectl apply -f -"

	if detail.Kubeconfig != "" {
		kubectlArgs += fmt.Sprintf(" --kubeconfig=%s", detail.Kubeconfig)
		applyArgs += fmt.Sprintf(" --kubeconfig=%s", detail.Kubeconfig)
	}

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

	return fingerprint, nil
}
