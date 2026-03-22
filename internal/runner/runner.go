package runner

import (
	"context"
	"log/slog"
	"time"

	"github.com/hysp/hycert-agent/internal/api"
	"github.com/hysp/hycert-agent/internal/config"
	"github.com/hysp/hycert-agent/internal/deployer"
	"github.com/hysp/hycert-agent/internal/model"
)

// Runner orchestrates the agent check-and-deploy cycle.
type Runner struct {
	cfg    *config.Config
	client *api.Client
	logger *slog.Logger
}

// New creates a Runner.
func New(cfg *config.Config, client *api.Client, logger *slog.Logger) *Runner {
	return &Runner{cfg: cfg, client: client, logger: logger}
}

// RunOnce executes a single check-and-deploy cycle.
func (r *Runner) RunOnce(ctx context.Context) {
	r.logger.Info("checking deployments", "hostname", r.cfg.Agent.Hostname)

	deployments, err := r.client.GetDeployments(r.cfg.Agent.Hostname)
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
