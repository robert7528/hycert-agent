package deployer

import (
	"fmt"

	"github.com/hysp/hycert-agent/internal/verify"
)

// VerifyError reports that the cert was written to disk but post-deploy
// TLS verification did not confirm the service is presenting it.
//
// The Fingerprint field holds what the agent wrote (its view of ground
// truth). The Result field classifies how the observed service state
// differs from expectation; callers use IsWarning() to decide whether to
// surface the deployment as success-with-warning or as an outright error.
//
// Semantics of Result values:
//   - ResultMismatch: service is up but serving a different cert →
//     misconfig (error alert).
//   - ResultConnRefused / ResultTimeout / ResultChainIncomplete: service
//     may still be coming up, or intermediate CA missing → warning.
type VerifyError struct {
	Fingerprint string              // cert we wrote (SHA-256 colon-hex)
	Result      verify.VerifyResult // classified observed state
	Detail      string              // human-readable explanation from ProbeTLS
	Actual      map[string]string   // per-SNI observed fingerprints (for debugging)
}

func (e *VerifyError) Error() string {
	return fmt.Sprintf("verify %s: %s", e.Result, e.Detail)
}

// IsWarning reports true when the VerifyResult should be surfaced as a
// warning (deployment is at-best partially applied; cert is on disk, may
// or may not be loaded yet) vs. an error alert.
//
// Explicit mapping (keep this table the single source of truth as new
// VerifyResult values are added):
//
//	Match            → never reaches here (no VerifyError created)
//	ConnRefused      → Warning (service may still be starting; next poll
//	                   will re-verify)
//	Timeout          → Warning (results were volatile inside probe window;
//	                   likely a timing issue, not misconfig)
//	Mismatch         → Error   (service is live and serving a DIFFERENT
//	                   cert — real misconfig, e.g. agent wrote to
//	                   /hyproxy/ssl/cert.pem but port 443 is nginx
//	                   reading from /etc/nginx/ssl/cert.pem)
//	ChainIncomplete  → Warning (cert served, intermediate CA missing —
//	                   operator should fix but service is functional for
//	                   clients that bundle roots; reserved, not yet
//	                   produced)
//	MTLSDetected     → Warning (cannot verify without client cert config;
//	                   operator should set SkipVerify=true or provide
//	                   client cert; reserved, not yet produced)
func (e *VerifyError) IsWarning() bool {
	switch e.Result {
	case verify.ResultMismatch:
		return false
	case verify.ResultConnRefused,
		verify.ResultTimeout,
		verify.ResultChainIncomplete,
		verify.ResultMTLSDetected:
		return true
	default:
		// Unknown result — treat as error so it's visible, not swallowed.
		return false
	}
}
