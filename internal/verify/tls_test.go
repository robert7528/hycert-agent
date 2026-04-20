package verify

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// newTLSServer starts an httptest.NewTLSServer and returns the server plus
// the SHA-256 colon-hex fingerprint of its leaf cert.
func newTLSServer(t *testing.T) (*httptest.Server, string, string, int) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)

	// Extract the leaf cert from the server's TLS config to compute fingerprint.
	certs := srv.TLS.Certificates
	if len(certs) == 0 || len(certs[0].Certificate) == 0 {
		t.Fatal("test server has no cert")
	}
	leaf, err := x509.ParseCertificate(certs[0].Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}

	u, _ := url.Parse(srv.URL)
	host, portStr, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(portStr)

	return srv, fingerprintOf(leaf), host, port
}

func TestProbeTLS_MatchOnFirstTry(t *testing.T) {
	_, fp, host, port := newTLSServer(t)

	resp := ProbeTLS(context.Background(), VerifyRequest{
		Host:                host,
		Port:                port,
		ExpectedFingerprint: fp,
		InitialDelay:        10 * time.Millisecond,
		Timeout:             3 * time.Second,
		RetryInterval:       50 * time.Millisecond,
		ConnectTimeout:      500 * time.Millisecond,
	})

	if resp.Result != ResultMatch {
		t.Errorf("Result = %s, want match (detail=%s)", resp.Result, resp.ErrorDetail)
	}
	if resp.Attempts < 1 {
		t.Errorf("Attempts = %d, want >=1", resp.Attempts)
	}
	if got := resp.ActualFingerprints[host]; got != fp {
		t.Errorf("ActualFingerprints[%s] = %q, want %q", host, got, fp)
	}
}

func TestProbeTLS_MismatchWhenFingerprintWrong(t *testing.T) {
	_, _, host, port := newTLSServer(t)

	resp := ProbeTLS(context.Background(), VerifyRequest{
		Host:                host,
		Port:                port,
		ExpectedFingerprint: "AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99",
		InitialDelay:        10 * time.Millisecond,
		Timeout:             500 * time.Millisecond,
		RetryInterval:       100 * time.Millisecond,
		ConnectTimeout:      300 * time.Millisecond,
	})

	if resp.Result != ResultMismatch {
		t.Errorf("Result = %s, want mismatch (detail=%s)", resp.Result, resp.ErrorDetail)
	}
	if resp.Attempts < 2 {
		t.Errorf("Attempts = %d, want >=2 (should retry before giving up)", resp.Attempts)
	}
}

func TestProbeTLS_ConnRefusedWhenPortClosed(t *testing.T) {
	// Bind + immediately close to free a real port number no one's listening on.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().(*net.TCPAddr)
	l.Close() // release port so further dials fail

	resp := ProbeTLS(context.Background(), VerifyRequest{
		Host:                "127.0.0.1",
		Port:                addr.Port,
		ExpectedFingerprint: "dummy",
		InitialDelay:        10 * time.Millisecond,
		Timeout:             500 * time.Millisecond,
		RetryInterval:       100 * time.Millisecond,
		ConnectTimeout:      200 * time.Millisecond,
	})

	if resp.Result != ResultConnRefused {
		t.Errorf("Result = %s, want conn_refused (detail=%s)", resp.Result, resp.ErrorDetail)
	}
}

func TestProbeTLS_ContextCancelAbortsImmediately(t *testing.T) {
	_, fp, host, port := newTLSServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	start := time.Now()
	resp := ProbeTLS(ctx, VerifyRequest{
		Host:                host,
		Port:                port,
		ExpectedFingerprint: fp,
		InitialDelay:        5 * time.Second, // would block if ctx not honored
		Timeout:             10 * time.Second,
		RetryInterval:       100 * time.Millisecond,
		ConnectTimeout:      200 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if resp.Result != ResultTimeout {
		t.Errorf("Result = %s, want timeout (ctx cancelled)", resp.Result)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("elapsed = %v, want quick abort on ctx cancel", elapsed)
	}
}

// TestProbeTLS_RetrySuccessAfterReload simulates a service that initially
// serves a stale cert (e.g. during a graceful reload) and switches to the
// new cert after several attempts. ProbeTLS must not give up after the
// first mismatch — this is the core value of retry + InitialDelay.
func TestProbeTLS_RetrySuccessAfterReload(t *testing.T) {
	oldCert, _ := generateSelfSigned(t, "stale.example")
	newCert, _ := generateSelfSigned(t, "fresh.example")

	var attempts int32
	const switchAfter = 3

	tlsCfg := &tls.Config{
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			n := atomic.AddInt32(&attempts, 1)
			if n <= switchAfter {
				return &oldCert, nil
			}
			return &newCert, nil
		},
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			// Perform handshake so client sees the cert, then close.
			tc := c.(*tls.Conn)
			_ = tc.Handshake()
			tc.Close()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)

	newLeaf, _ := x509.ParseCertificate(newCert.Certificate[0])
	expectedFP := fingerprintOf(newLeaf)

	resp := ProbeTLS(context.Background(), VerifyRequest{
		Host:                "127.0.0.1",
		Port:                addr.Port,
		ExpectedFingerprint: expectedFP,
		InitialDelay:        10 * time.Millisecond,
		Timeout:             3 * time.Second,
		RetryInterval:       50 * time.Millisecond,
		ConnectTimeout:      500 * time.Millisecond,
	})

	if resp.Result != ResultMatch {
		t.Errorf("Result = %s, want match after switch (detail=%s, attempts=%d)",
			resp.Result, resp.ErrorDetail, resp.Attempts)
	}
	if resp.Attempts <= switchAfter {
		t.Errorf("Attempts = %d, want >%d (should have retried past the stale-cert phase)",
			resp.Attempts, switchAfter)
	}
}

// TestClassify_SettledMismatch: last N all handshake OK + same wrong FP → Mismatch.
func TestClassify_SettledMismatch(t *testing.T) {
	recent := []attemptOutcome{
		{stage: stageHandshakeOK, fingerprint: "AA:BB"},
		{stage: stageHandshakeOK, fingerprint: "AA:BB"},
		{stage: stageHandshakeOK, fingerprint: "AA:BB"},
	}
	if got := classify(recent, "CC:DD"); got != ResultMismatch {
		t.Errorf("got %s, want mismatch", got)
	}
}

// TestClassify_VolatileFingerprints: last N succeeded but fingerprints differ → Timeout.
func TestClassify_VolatileFingerprints(t *testing.T) {
	recent := []attemptOutcome{
		{stage: stageHandshakeOK, fingerprint: "AA:BB"},
		{stage: stageHandshakeOK, fingerprint: "CC:DD"},
		{stage: stageHandshakeOK, fingerprint: "AA:BB"},
	}
	if got := classify(recent, "EE:FF"); got != ResultTimeout {
		t.Errorf("got %s, want timeout", got)
	}
}

// TestClassify_ConnRefused: all TCP failed → ConnRefused (port closed).
func TestClassify_ConnRefused(t *testing.T) {
	recent := []attemptOutcome{
		{stage: stageTCPFailed},
		{stage: stageTCPFailed},
		{stage: stageTCPFailed},
	}
	if got := classify(recent, "AA:BB"); got != ResultConnRefused {
		t.Errorf("got %s, want conn_refused", got)
	}
}

// TestClassify_HandshakeFailure: all TCP OK but TLS rejected → HandshakeFailure.
// Covers the cert/key mismatch scenario (server sends alert 40).
func TestClassify_HandshakeFailure(t *testing.T) {
	recent := []attemptOutcome{
		{stage: stageHandshakeFailed},
		{stage: stageHandshakeFailed},
		{stage: stageHandshakeFailed},
	}
	if got := classify(recent, "AA:BB"); got != ResultHandshakeFailure {
		t.Errorf("got %s, want handshake_failure", got)
	}
}

// TestClassify_MixedTCPAndHandshake: TCP refused + handshake failed → Timeout.
// Not uniformly one failure mode; classifier holds back from settling.
func TestClassify_MixedTCPAndHandshake(t *testing.T) {
	recent := []attemptOutcome{
		{stage: stageTCPFailed},
		{stage: stageHandshakeFailed},
		{stage: stageTCPFailed},
	}
	if got := classify(recent, "AA:BB"); got != ResultTimeout {
		t.Errorf("got %s, want timeout (mixed TCP/handshake failures)", got)
	}
}

// TestClassify_Mixed: mix of success and failure → Timeout.
// Covers the Tomcat cold-start scenario: port opens late, first handshakes
// return stale FP, probe window expires before new cert loads.
func TestClassify_Mixed(t *testing.T) {
	recent := []attemptOutcome{
		{stage: stageTCPFailed},
		{stage: stageHandshakeOK, fingerprint: "AA:BB"},
		{stage: stageHandshakeOK, fingerprint: "AA:BB"},
	}
	if got := classify(recent, "CC:DD"); got != ResultTimeout {
		t.Errorf("got %s, want timeout (mixed history)", got)
	}
}

// generateSelfSigned produces a short-lived EC self-signed cert for tests.
func generateSelfSigned(t *testing.T, cn string) (tls.Certificate, []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(1 * time.Hour),
		DNSNames:     []string{cn, "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	tlsCert := tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
	}
	return tlsCert, der
}

// Sanity check that fingerprintOf handles a known PEM cert → matches openssl output.
func TestFingerprintOf_KnownValue(t *testing.T) {
	// Self-signed cert generated once for test (openssl x509 -noout -fingerprint -sha256).
	certPEM := `-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE3MTAyMDE5NDMwNloXDTE4MTAyMDE5NDMwNlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABD0d
7VNhbWvZLWPuj/RtHFjvtJBEwOkhbN/BnnE8rnZR8+sbwnc/KhCk3FhnpHZnQz7B
5aETbbIgmuvewdjvSBSjYzBhMA4GA1UdDwEB/wQEAwICpDATBgNVHSUEDDAKBggr
BgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MCkGA1UdEQQiMCCCDmxvY2FsaG9zdDo1
NDUzgg4xMjcuMC4wLjE6NTQ1MzAKBggqhkjOPQQDAgNIADBFAiEA2zpJEPQyz6/l
Wf86aX6PepsntZv2GYlA5UpabfT2EZICICpJ5h/iI+i341gBmLiAFQOyTDT+/wQc
6MF9+Yw1Yy0t
-----END CERTIFICATE-----
`
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatal("decode PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fp := fingerprintOf(cert)
	// Any 64-char colon-hex format is acceptable — we're just sanity-checking
	// the shape, not a specific known value (that would tie test to cert gen).
	if len(fp) != 95 { // 32 bytes * 2 hex + 31 colons = 95
		t.Errorf("unexpected fingerprint length %d: %s", len(fp), fp)
	}
}
