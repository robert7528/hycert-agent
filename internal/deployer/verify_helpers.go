package deployer

import (
	"crypto/x509"
	"time"

	"github.com/hysp/hycert-agent/internal/model"
)

// These helpers build the verify.VerifyRequest fields from the deployment
// target's target_detail + *x509.Certificate. Shared across all deployers
// so PEM / JKS / PFX / K8S get the same defaults and override semantics
// without duplicating the logic.

// verifyHost returns the TLS probe target host.
// Defaults to 127.0.0.1 (agent probes itself on the same host).
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

// sniList builds the SNI list for probing. Current skeleton returns at
// most one SNI (first concrete non-wildcard SAN or CN); parallel multi-
// SNI probing is a later step.
func sniList(cert *x509.Certificate) []string {
	if sni := pickSNI(cert); sni != "" {
		return []string{sni}
	}
	return nil
}

// verifyTimeoutFor picks a reasonable total probe budget per service
// type, unless the deployment overrides it explicitly.
//
// These are the "how long to wait for the service to actually serve the
// new cert" numbers — longer than reload timeout because reload command
// may return fast (SCM accepts the restart) while the service is still
// warming up (JVM class loading, app-pool recycling).
//
//	tomcat / iis : 180s (JVM / app pool cold start)
//	kubernetes   :  60s (kubectl apply + Ingress controller reload)
//	others       :  30s (nginx reload is near-instant)
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
