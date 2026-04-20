package deployer

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
)

// parseLeafCert decodes the first PEM block from pemData and parses it as
// an X.509 certificate. Returns the parsed leaf.
func parseLeafCert(pemData []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return cert, nil
}

// fingerprintFromCert returns SHA-256 colon-hex fingerprint (e.g.
// "AA:BB:..") of the given cert. Matches hycert-api's format.
func fingerprintFromCert(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	hex := fmt.Sprintf("%X", sum)
	parts := make([]string, 0, len(hex)/2)
	for i := 0; i < len(hex); i += 2 {
		parts = append(parts, hex[i:i+2])
	}
	return strings.Join(parts, ":")
}

// computeFingerprint is kept for backward compatibility with deployers
// that have not been refactored yet (jks, pfx, k8s, pem_combined). New
// code should use parseLeafCert + fingerprintFromCert so the parsed
// certificate can be reused for SNI selection and chain validation.
//
// Deprecated: prefer parseLeafCert + fingerprintFromCert.
func computeFingerprint(pemData []byte) (string, error) {
	cert, err := parseLeafCert(pemData)
	if err != nil {
		return "", err
	}
	return fingerprintFromCert(cert), nil
}

// pickSNI returns the first concrete (non-wildcard) DNS name suitable for
// sending as TLS SNI when probing. Order of preference:
//  1. First non-wildcard SAN.
//  2. CN, if non-wildcard.
//  3. Empty string (caller falls back to the probe Host).
//
// Wildcards are skipped because most TLS servers match SNI as literal
// hostnames — sending "*.example.com" as SNI typically matches no server
// block and returns a default (often wrong) cert.
func pickSNI(cert *x509.Certificate) string {
	for _, san := range cert.DNSNames {
		if san != "" && !strings.HasPrefix(san, "*.") {
			return san
		}
	}
	if cn := cert.Subject.CommonName; cn != "" && !strings.HasPrefix(cn, "*.") {
		return cn
	}
	return ""
}
