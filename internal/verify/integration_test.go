//go:build integration

// Integration tests for verify.ProbeTLS against the testenv harness.
//
// These tests are opt-in via the `integration` build tag so a plain
// `go test ./...` never tries to reach localhost:8443 etc.
//
// Usage:
//
//	cd testenv && make up
//	go test -tags integration ./internal/verify/... -v
//
// Requires environment variables exported by the harness:
//
//	HYCERT_TEST_FP_A  — SHA-256 colon-hex fingerprint of cert-a.pem
//	HYCERT_TEST_FP_B  — SHA-256 colon-hex fingerprint of cert-b.pem
//
// Quick setup:
//
//	export HYCERT_TEST_FP_A=$(cd testenv && make -s fingerprint-a)
//	export HYCERT_TEST_FP_B=$(cd testenv && make -s fingerprint-b)
//
// The tests assert VerifyResult per scenario AND print timing data
// (Elapsed, Attempts) so manual calibration of InitialDelay / Timeout
// can reference real numbers instead of guesswork.

package verify

import (
	"context"
	"os"
	"testing"
	"time"
)

func envFP(t *testing.T, name string) string {
	t.Helper()
	fp := os.Getenv(name)
	if fp == "" {
		t.Fatalf("%s not set — did you export it from testenv?", name)
	}
	return fp
}

func probeFast(host string, port int, expected string, sni []string) VerifyResponse {
	return ProbeTLS(context.Background(), VerifyRequest{
		Host:                host,
		Port:                port,
		ExpectedFingerprint: expected,
		SNIList:             sni,
		InitialDelay:        100 * time.Millisecond, // testenv is already up
		Timeout:             5 * time.Second,
		RetryInterval:       500 * time.Millisecond,
		ConnectTimeout:      1 * time.Second,
	})
}

func TestIntegration_Match(t *testing.T) {
	fpA := envFP(t, "HYCERT_TEST_FP_A")
	resp := probeFast("127.0.0.1", 8443, fpA, []string{"test-a.local"})
	t.Logf("result=%s attempts=%d elapsed=%v detail=%q",
		resp.Result, resp.Attempts, resp.Elapsed, resp.ErrorDetail)

	if resp.Result != ResultMatch {
		t.Errorf("want match, got %s (detail: %s)", resp.Result, resp.ErrorDetail)
	}
}

func TestIntegration_Mismatch(t *testing.T) {
	fpA := envFP(t, "HYCERT_TEST_FP_A")
	// Deploy cert-a but probe nginx-b (serving cert-b) — fingerprint differs.
	resp := probeFast("127.0.0.1", 8444, fpA, []string{"test-a.local"})
	t.Logf("result=%s attempts=%d elapsed=%v actual=%v detail=%q",
		resp.Result, resp.Attempts, resp.Elapsed, resp.ActualFingerprints, resp.ErrorDetail)

	if resp.Result != ResultMismatch {
		t.Errorf("want mismatch, got %s (detail: %s)", resp.Result, resp.ErrorDetail)
	}
}

func TestIntegration_ConnRefused(t *testing.T) {
	fpA := envFP(t, "HYCERT_TEST_FP_A")
	resp := probeFast("127.0.0.1", 9999, fpA, []string{"test-a.local"})
	t.Logf("result=%s attempts=%d elapsed=%v detail=%q",
		resp.Result, resp.Attempts, resp.Elapsed, resp.ErrorDetail)

	if resp.Result != ResultConnRefused {
		t.Errorf("want conn_refused, got %s (detail: %s)", resp.Result, resp.ErrorDetail)
	}
}

func TestIntegration_HandshakeFailure(t *testing.T) {
	fpA := envFP(t, "HYCERT_TEST_FP_A")
	// nginx-mismatch: cert-a public + key-b private. Server can't complete
	// key exchange → alert 40 handshake_failure.
	resp := probeFast("127.0.0.1", 8445, fpA, []string{"test-a.local"})
	t.Logf("result=%s attempts=%d elapsed=%v detail=%q",
		resp.Result, resp.Attempts, resp.Elapsed, resp.ErrorDetail)

	if resp.Result != ResultHandshakeFailure {
		t.Errorf("want handshake_failure, got %s (detail: %s)", resp.Result, resp.ErrorDetail)
	}
}
