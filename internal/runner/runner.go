package runner

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"runtime"
	"time"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/config"
	"github.com/hysp/hycert-agent/internal/deployer"
	"github.com/hysp/hycert-agent/internal/model"
)

// Runner orchestrates the agent check-and-deploy cycle.
type Runner struct {
	cfg     *config.Config
	client  *api.Client
	logger  *slog.Logger
	version string
}

// New creates a Runner.
func New(cfg *config.Config, client *api.Client, logger *slog.Logger, version string) *Runner {
	return &Runner{cfg: cfg, client: client, logger: logger, version: version}
}

// RunOnce executes a single check-and-deploy cycle.
func (r *Runner) RunOnce(ctx context.Context) {
	r.logger.Info("checking deployments", "agent_id", r.cfg.Agent.AgentID)

	deployments, err := r.client.GetDeployments()
	if err != nil {
		r.logger.Error("failed to get deployments", "error", err)
		return
	}

	if len(deployments) == 0 {
		r.logger.Info("no deployments found")
		return
	}

	r.logger.Info("found deployments", "count", len(deployments))

	var deployed, skipped, failed int
	for _, dep := range deployments {
		result := r.processDeploy(ctx, dep)
		switch result {
		case "deployed":
			deployed++
		case "skipped":
			skipped++
		case "failed":
			failed++
		}
	}

	r.logger.Info("cycle complete",
		"deployed", deployed,
		"skipped", skipped,
		"failed", failed,
	)
}

func (r *Runner) processDeploy(ctx context.Context, dep model.AgentDeployment) string {
	log := r.logger.With(
		"deployment_id", dep.ID,
		"cert_id", dep.CertificateID,
		"service", dep.TargetService,
		"host", dep.TargetHost,
	)

	// Skip if fingerprint matches and already deployed
	if dep.CertFingerprint == dep.LastFingerprint && dep.DeployStatus == "deployed" {
		log.Debug("skipping, already up to date", "fingerprint", dep.CertFingerprint)
		return "skipped"
	}

	log.Info("deploying",
		"cert_fingerprint", dep.CertFingerprint,
		"last_fingerprint", dep.LastFingerprint,
		"status", dep.DeployStatus,
	)

	start := time.Now()

	d, err := deployer.Get(dep.TargetService, r.cfg.Agent.Backup, r.cfg.Agent.BackupDir)
	if err != nil {
		r.reportFailure(dep.ID, err, start, log)
		return "failed"
	}

	fingerprint, err := d.Deploy(ctx, r.client, dep)
	if err != nil {
		r.reportFailure(dep.ID, err, start, log)
		return "failed"
	}

	duration := int(time.Since(start).Milliseconds())
	if err := r.client.UpdateDeployStatus(dep.ID, model.UpdateStatusRequest{
		Action:      "deploy",
		Status:      "success",
		Fingerprint: fingerprint,
		DurationMs:  &duration,
	}); err != nil {
		log.Error("failed to report success", "error", err)
		return "failed"
	}

	log.Info("deployed successfully", "fingerprint", fingerprint, "duration_ms", duration)
	return "deployed"
}

func (r *Runner) reportFailure(deployID uint, deployErr error, start time.Time, log *slog.Logger) {
	duration := int(time.Since(start).Milliseconds())
	errMsg := deployErr.Error()
	log.Error("deployment failed", "error", errMsg, "duration_ms", duration)

	if err := r.client.UpdateDeployStatus(deployID, model.UpdateStatusRequest{
		Action:       "deploy",
		Status:       "failed",
		ErrorMessage: errMsg,
		DurationMs:   &duration,
	}); err != nil {
		log.Error("failed to report failure", "error", err)
	}
}

// Register sends agent registration to the server. Called on every startup (upsert).
func (r *Runner) Register(ctx context.Context) error {
	req := model.RegisterRequest{
		AgentID:     r.cfg.Agent.AgentID,
		Name:        r.cfg.Agent.Name,
		Hostname:    r.cfg.Agent.Hostname,
		IPAddresses: getLocalIPs(),
		OS:          runtime.GOOS,
		Version:     r.version,
	}
	resp, err := r.client.Register(req)
	if err != nil {
		return fmt.Errorf("agent registration failed: %w", err)
	}
	r.logger.Info("agent registered",
		"agent_id", resp.AgentID,
		"name", resp.Name,
		"status", resp.Status,
	)
	return nil
}

// getLocalIPs returns non-loopback IPv4 addresses.
func getLocalIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			ips = append(ips, ipnet.IP.String())
		}
	}
	return ips
}
