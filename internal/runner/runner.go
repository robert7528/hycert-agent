package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime"
	"strings"
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
	// Register/heartbeat: update last_seen_at on every cycle
	if err := r.Register(ctx); err != nil {
		r.logger.Warn("registration failed", "error", err)
	}

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

	// Verification disagreement: cert was written to disk but the live TLS
	// probe didn't confirm it. For warnings (service still starting,
	// verify window ran out) we mark deployed so the fingerprint
	// propagates and the next poll skips. For Mismatch (service live,
	// serving a different cert) we treat as a real failure.
	var verifyErr *deployer.VerifyError
	if errors.As(err, &verifyErr) {
		if verifyErr.IsWarning() {
			return r.reportVerifyWarning(dep.ID, verifyErr, start, log)
		}
		return r.reportVerifyMismatch(dep.ID, verifyErr, start, log)
	}

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

// reportVerifyWarning is used when the cert was written to disk but the
// TLS probe didn't conclusively match (ConnRefused, Timeout). We propagate
// the fingerprint so last_fingerprint updates — next poll will skip the
// deploy entirely if the service caught up in the meantime; if it didn't,
// subsequent polls will resurface the problem.
func (r *Runner) reportVerifyWarning(deployID uint, ve *deployer.VerifyError, start time.Time, log *slog.Logger) string {
	duration := int(time.Since(start).Milliseconds())
	log.Warn("deployment completed with verify warning",
		"verify_result", ve.Result.String(),
		"verify_detail", ve.Detail,
		"actual_fingerprints", ve.Actual,
		"duration_ms", duration,
	)

	if err := r.client.UpdateDeployStatus(deployID, model.UpdateStatusRequest{
		Action:       "deploy",
		Status:       "success",
		Fingerprint:  ve.Fingerprint,
		ErrorMessage: ve.Error(),
		DurationMs:   &duration,
	}); err != nil {
		log.Error("failed to report verify warning", "error", err)
		return "failed"
	}
	return "deployed"
}

// reportVerifyMismatch is used when verification revealed a hard error
// (Mismatch: service live but serving a different cert; HandshakeFailure:
// TCP OK but TLS rejected e.g. from cert/key mismatch or cipher / curve
// incompatibility). We do NOT update last_fingerprint so the deployment
// stays visibly "failed" until an operator intervenes.
func (r *Runner) reportVerifyMismatch(deployID uint, ve *deployer.VerifyError, start time.Time, log *slog.Logger) string {
	duration := int(time.Since(start).Milliseconds())
	log.Error("deployment verify failed",
		"verify_result", ve.Result.String(),
		"expected_fingerprint", ve.Fingerprint,
		"actual_fingerprints", ve.Actual,
		"verify_detail", ve.Detail,
		"duration_ms", duration,
	)

	if err := r.client.UpdateDeployStatus(deployID, model.UpdateStatusRequest{
		Action:       "deploy",
		Status:       "failed",
		ErrorMessage: ve.Error(),
		DurationMs:   &duration,
	}); err != nil {
		log.Error("failed to report verify mismatch", "error", err)
	}
	return "failed"
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
		Interval:    r.cfg.Agent.Interval,
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

// getLocalIPs returns non-loopback, non-virtual IPv4 addresses.
// Filters out container/bridge interfaces (docker, podman, veth, br-, virbr).
func getLocalIPs() []string {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		// Skip down, loopback, and virtual interfaces
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		if strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") ||
			strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "virbr") ||
			strings.HasPrefix(name, "podman") || strings.HasPrefix(name, "cni") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return ips
}
