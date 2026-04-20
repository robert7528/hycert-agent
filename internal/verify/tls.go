// Package verify provides active post-deployment verification by probing
// the target service's TLS port and comparing the presented certificate
// fingerprint against the expected one.
//
// This module is the agent's source of truth for "deployment succeeded":
// writing bytes to disk is not enough — the service must actually serve the
// new certificate. Callers (deployers) invoke ProbeTLS after reload to
// confirm the new cert is being presented.
//
// Scope of this skeleton (PR #1 step 2):
//   - Single SNI (req.SNIList[0] if non-empty, else req.Host)
//   - No mTLS detection (treated as ConnRefused if handshake fails)
//   - No strict chain check (InsecureSkipVerify always true)
//   - No parallel per-SNI probing
//
// Later steps add: multi-SNI parallel, wildcard filtering, mTLS detect,
// chain completeness check.
package verify

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// VerifyResult is the categorized outcome of a probe cycle.
type VerifyResult int

const (
	// ResultMatch — at least one successful handshake, presented fingerprint
	// matches ExpectedFingerprint.
	ResultMatch VerifyResult = iota

	// ResultConnRefused — never completed a TLS handshake within the
	// probe window. Typical cause: service still restarting or port blocked.
	ResultConnRefused

	// ResultMismatch — at least one handshake succeeded, but every
	// presented fingerprint differed from ExpectedFingerprint. Typical
	// cause: another service owns the port (e.g. an LB fronting the agent's
	// target, or a stale reload never applied the new cert).
	ResultMismatch

	// ResultChainIncomplete — (reserved for later step) fingerprint
	// matched but strict chain validation failed.
	ResultChainIncomplete

	// ResultMTLSDetected — (reserved for later step) server requires a
	// client certificate; probe cannot confirm fingerprint.
	ResultMTLSDetected

	// ResultTimeout — mixed outcomes over the probe window. Used when the
	// loop could neither conclude Match, nor settle on ConnRefused /
	// Mismatch consistently.
	ResultTimeout
)

// String returns a short label for logging.
func (r VerifyResult) String() string {
	switch r {
	case ResultMatch:
		return "match"
	case ResultConnRefused:
		return "conn_refused"
	case ResultMismatch:
		return "mismatch"
	case ResultChainIncomplete:
		return "chain_incomplete"
	case ResultMTLSDetected:
		return "mtls_detected"
	case ResultTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// VerifyRequest configures a probe.
type VerifyRequest struct {
	Host                string   // target host (defaults to "127.0.0.1" if empty)
	Port                int      // target TLS port
	SNIList             []string // SNI to send; empty → use Host
	ExpectedFingerprint string   // SHA-256 fingerprint in colon-hex ("AA:BB:...")

	InitialDelay   time.Duration // wait before first probe (service reload latency)
	Timeout        time.Duration // total probe budget
	RetryInterval  time.Duration // gap between attempts
	ConnectTimeout time.Duration // per-Dial network timeout
}

// VerifyResponse is the structured outcome.
type VerifyResponse struct {
	Result             VerifyResult
	ActualFingerprints map[string]string // SNI → observed fingerprint (colon-hex)
	Attempts           int
	Elapsed            time.Duration
	ErrorDetail        string
}

// Defaults applied if a field is zero.
const (
	DefaultInitialDelay   = 2 * time.Second
	DefaultTimeout        = 180 * time.Second
	DefaultRetryInterval  = 2 * time.Second
	DefaultConnectTimeout = 5 * time.Second

	// classifyWindow is the number of most-recent attempts examined when
	// deciding between Mismatch (settled-on-wrong-cert) and Timeout
	// (results still volatile). Small enough to fit inside a short probe
	// window; large enough that a single one-off fluke doesn't flip the
	// result.
	classifyWindow = 3
)

// ProbeTLS repeatedly connects to host:port via TLS until the presented
// fingerprint matches ExpectedFingerprint, classifiable failure is reached,
// or ctx/Timeout expires.
//
// Semantics of ctx vs Timeout:
//   - ctx is a hard stop (runner shutdown, deployment cancel) — probe returns
//     immediately with ResultTimeout when ctx is cancelled.
//   - Timeout is the probe's own budget — internally ProbeTLS wraps ctx with
//     a deadline. Whichever expires first wins.
func ProbeTLS(ctx context.Context, req VerifyRequest) VerifyResponse {
	req = applyDefaults(req)
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	resp := VerifyResponse{
		ActualFingerprints: map[string]string{},
	}

	// Initial delay before first probe.
	select {
	case <-time.After(req.InitialDelay):
	case <-ctx.Done():
		resp.Result = ResultTimeout
		resp.ErrorDetail = "context cancelled during initial delay"
		resp.Elapsed = time.Since(start)
		return resp
	}

	sni := firstSNI(req)

	var (
		recent  = make([]attemptOutcome, 0, classifyWindow)
		lastErr error
	)

	for {
		resp.Attempts++
		out := dialAndCheck(ctx, req, sni)

		// Record attempt in the rolling window of size classifyWindow.
		ao := attemptOutcome{handshakeOK: out.err == nil, fingerprint: out.fingerprint}
		if len(recent) == classifyWindow {
			recent = recent[1:]
		}
		recent = append(recent, ao)

		if out.err == nil {
			resp.ActualFingerprints[sni] = out.fingerprint
			if out.fingerprint == req.ExpectedFingerprint {
				resp.Result = ResultMatch
				resp.Elapsed = time.Since(start)
				return resp
			}
			lastErr = fmt.Errorf("fingerprint mismatch: got %s, want %s",
				out.fingerprint, req.ExpectedFingerprint)
		} else {
			lastErr = out.err
		}

		select {
		case <-time.After(req.RetryInterval):
		case <-ctx.Done():
			resp.Result = classify(recent, req.ExpectedFingerprint)
			resp.Elapsed = time.Since(start)
			if lastErr != nil {
				resp.ErrorDetail = lastErr.Error()
			}
			return resp
		}
	}
}

// attemptOutcome is one probe attempt's outcome, kept in a rolling window
// for terminal classification.
type attemptOutcome struct {
	handshakeOK bool
	fingerprint string
}

// dialOutcome is the per-attempt result.
type dialOutcome struct {
	fingerprint string
	err         error
}

func dialAndCheck(ctx context.Context, req VerifyRequest, sni string) dialOutcome {
	addr := net.JoinHostPort(req.Host, fmt.Sprintf("%d", req.Port))
	dialer := &net.Dialer{Timeout: req.ConnectTimeout}

	dialCtx, cancel := context.WithTimeout(ctx, req.ConnectTimeout)
	defer cancel()

	conn, err := tls.DialWithDialer(
		dialerFromContext(dialer, dialCtx),
		"tcp",
		addr,
		&tls.Config{
			InsecureSkipVerify: true, // we verify by fingerprint ourselves
			ServerName:         sni,
		},
	)
	if err != nil {
		return dialOutcome{err: err}
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return dialOutcome{err: errors.New("no peer certificates")}
	}
	return dialOutcome{fingerprint: fingerprintOf(state.PeerCertificates[0])}
}

// dialerFromContext returns a Dialer whose Dial methods honor ctx. net.Dialer
// has DialContext but tls.DialWithDialer wants a non-context Dialer; we wrap
// Deadline from ctx onto the dialer. This is good enough for the skeleton —
// parallel probes in a later step will use tls.Dialer.DialContext directly.
func dialerFromContext(d *net.Dialer, ctx context.Context) *net.Dialer {
	if dl, ok := ctx.Deadline(); ok {
		d.Deadline = dl
	}
	return d
}

// firstSNI returns the SNI to send: SNIList[0] if present, else Host.
func firstSNI(req VerifyRequest) string {
	if len(req.SNIList) > 0 && req.SNIList[0] != "" {
		return req.SNIList[0]
	}
	return req.Host
}

// fingerprintOf returns "AA:BB:..." SHA-256 of the cert DER.
func fingerprintOf(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	hex := fmt.Sprintf("%X", sum)
	parts := make([]string, 0, len(hex)/2)
	for i := 0; i < len(hex); i += 2 {
		parts = append(parts, hex[i:i+2])
	}
	return strings.Join(parts, ":")
}

// classify resolves the terminal Result when the probe loop exits without
// a match, using the last N attempts' pattern.
//
// Rules (in order):
//  1. No attempts recorded → Timeout (pathological).
//  2. All recent attempts failed handshake → ConnRefused (service down).
//  3. All recent attempts succeeded + same non-expected fingerprint →
//     Mismatch (settled on wrong cert; likely misconfig).
//  4. Anything else (mixed success/failure, volatile fingerprints) →
//     Timeout (still transitioning; probe window was too short).
//
// The "last N" framing matters because a single late handshake that catches
// an in-flight reload shouldn't flip the result from Timeout to Mismatch —
// Mismatch raises an error alert, Timeout raises only a warning.
func classify(recent []attemptOutcome, expected string) VerifyResult {
	if len(recent) == 0 {
		return ResultTimeout
	}

	allHandshakeFailed := true
	allHandshakeOK := true
	firstFP := ""
	uniformFP := true

	for i, a := range recent {
		if a.handshakeOK {
			allHandshakeFailed = false
			if i == 0 || firstFP == "" {
				firstFP = a.fingerprint
			} else if a.fingerprint != firstFP {
				uniformFP = false
			}
		} else {
			allHandshakeOK = false
		}
	}

	switch {
	case allHandshakeFailed:
		return ResultConnRefused
	case allHandshakeOK && uniformFP && firstFP != "" && firstFP != expected:
		return ResultMismatch
	default:
		return ResultTimeout
	}
}

func applyDefaults(req VerifyRequest) VerifyRequest {
	if req.Host == "" {
		req.Host = "127.0.0.1"
	}
	if req.InitialDelay <= 0 {
		req.InitialDelay = DefaultInitialDelay
	}
	if req.Timeout <= 0 {
		req.Timeout = DefaultTimeout
	}
	if req.RetryInterval <= 0 {
		req.RetryInterval = DefaultRetryInterval
	}
	if req.ConnectTimeout <= 0 {
		req.ConnectTimeout = DefaultConnectTimeout
	}
	return req
}
