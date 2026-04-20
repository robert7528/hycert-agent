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

	// ResultConnRefused — TCP connect never succeeded within the probe
	// window. Typical cause: service down, port not yet open after reload,
	// or firewall. Treated as a warning (may resolve itself next poll).
	ResultConnRefused

	// ResultHandshakeFailure — TCP connect succeeded but TLS handshake was
	// rejected by the server (e.g. alert 40). Typical causes: cert/key
	// mismatch on disk, cipher incompatibility, SNI matches no server
	// block and server drops the handshake. Treated as an error — this is
	// misconfig, not a transient state.
	ResultHandshakeFailure

	// ResultMismatch — handshake succeeded, server presented a cert, but
	// the fingerprint differed from ExpectedFingerprint. Typical cause:
	// another service owns the port (LB fronting, stale nginx config) or
	// the agent wrote to a path the service doesn't read.
	ResultMismatch

	// ResultChainIncomplete — (reserved for later step) fingerprint
	// matched but strict chain validation failed.
	ResultChainIncomplete

	// ResultMTLSDetected — (reserved for later step) server requires a
	// client certificate; probe cannot confirm fingerprint.
	ResultMTLSDetected

	// ResultTimeout — mixed outcomes over the probe window. Used when the
	// loop could neither conclude Match nor settle consistently on one of
	// the failure classes.
	ResultTimeout
)

// String returns a short label for logging.
func (r VerifyResult) String() string {
	switch r {
	case ResultMatch:
		return "match"
	case ResultConnRefused:
		return "conn_refused"
	case ResultHandshakeFailure:
		return "handshake_failure"
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
		ao := attemptOutcome{stage: out.stage, fingerprint: out.fingerprint}
		if len(recent) == classifyWindow {
			recent = recent[1:]
		}
		recent = append(recent, ao)

		if out.stage == stageHandshakeOK {
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

// attemptStage describes which phase of a probe attempt reached its
// final outcome. Used by classify() to distinguish TCP-level refusal
// from TLS-layer rejection, which have different operator-visible
// meanings.
type attemptStage int

const (
	stageTCPFailed       attemptStage = iota // TCP dial failed (port closed, timeout, unreachable)
	stageHandshakeFailed                     // TCP OK, TLS handshake rejected
	stageHandshakeOK                         // TCP OK, TLS OK, cert read
)

// attemptOutcome is one probe attempt's outcome, kept in a rolling window
// for terminal classification.
type attemptOutcome struct {
	stage       attemptStage
	fingerprint string // populated only when stage == stageHandshakeOK
}

// dialOutcome is the per-attempt result. stage classifies how far the
// attempt progressed before failing (or succeeding); err carries the
// underlying error for diagnostics.
type dialOutcome struct {
	stage       attemptStage
	fingerprint string
	err         error
}

// dialAndCheck performs a TCP dial, then a TLS handshake, then reads the
// peer cert. Splitting TCP from TLS lets us distinguish port-closed from
// TLS-rejected in classification (different operator diagnostic paths).
func dialAndCheck(ctx context.Context, req VerifyRequest, sni string) dialOutcome {
	addr := net.JoinHostPort(req.Host, fmt.Sprintf("%d", req.Port))

	// --- Stage 1: TCP connect ---
	dialer := &net.Dialer{Timeout: req.ConnectTimeout}
	tcp, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return dialOutcome{stage: stageTCPFailed, err: err}
	}
	defer tcp.Close()

	// --- Stage 2: TLS handshake ---
	tlsConn := tls.Client(tcp, &tls.Config{
		InsecureSkipVerify: true, // we verify by fingerprint ourselves
		ServerName:         sni,
	})
	if err := tlsConn.SetDeadline(time.Now().Add(req.ConnectTimeout)); err != nil {
		return dialOutcome{stage: stageHandshakeFailed, err: err}
	}
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return dialOutcome{stage: stageHandshakeFailed, err: err}
	}

	// --- Stage 3: read cert ---
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return dialOutcome{stage: stageHandshakeFailed, err: errors.New("no peer certificates")}
	}
	return dialOutcome{
		stage:       stageHandshakeOK,
		fingerprint: fingerprintOf(state.PeerCertificates[0]),
	}
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
// Rules (in order of precedence):
//  1. No attempts                           → Timeout (pathological).
//  2. All recent attempts failed at TCP     → ConnRefused
//     (service down / port closed; warning).
//  3. All recent attempts failed at TLS
//     handshake                             → HandshakeFailure
//     (TCP OK but TLS rejected — cert/key mismatch, cipher/SNI misconfig;
//     error).
//  4. All recent attempts completed
//     handshake + same non-expected FP      → Mismatch
//     (settled on wrong cert; error).
//  5. Anything else (mixed stages, volatile
//     fingerprints)                         → Timeout
//     (still transitioning; warning).
//
// The "last N" framing matters because a single late success that catches
// an in-flight reload shouldn't flip the result from Timeout to Mismatch —
// errors raise alerts, warnings don't.
func classify(recent []attemptOutcome, expected string) VerifyResult {
	if len(recent) == 0 {
		return ResultTimeout
	}

	var (
		tcpFailedCount       int
		handshakeFailedCount int
		handshakeOKCount     int
		firstFP              string
		uniformFP            = true
	)

	for _, a := range recent {
		switch a.stage {
		case stageTCPFailed:
			tcpFailedCount++
		case stageHandshakeFailed:
			handshakeFailedCount++
		case stageHandshakeOK:
			handshakeOKCount++
			if firstFP == "" {
				firstFP = a.fingerprint
			} else if a.fingerprint != firstFP {
				uniformFP = false
			}
		}
	}

	total := len(recent)
	switch {
	case tcpFailedCount == total:
		return ResultConnRefused
	case handshakeFailedCount == total:
		return ResultHandshakeFailure
	case handshakeOKCount == total && uniformFP && firstFP != "" && firstFP != expected:
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
