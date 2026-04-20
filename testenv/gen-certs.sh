#!/usr/bin/env bash
# Generate self-signed EC P-256 certs for the test harness.
#
# Produces in $1:
#   cert-a.pem / key-a.pem  — self-signed, CN=test-a.local, SAN=test-a.local
#   cert-b.pem / key-b.pem  — self-signed, CN=test-b.local, SAN=test-b.local
#
# The mismatch scenario reuses cert-a.pem + key-b.pem (wrong key for that
# cert — TLS handshake will fail with alert 40). No separate artifact.

set -euo pipefail
OUT="${1:?usage: gen-certs.sh <output-dir>}"
mkdir -p "$OUT"

gen() {
    local name="$1" cn="$2"
    if [[ -f "$OUT/cert-$name.pem" && -f "$OUT/key-$name.pem" ]]; then
        echo "  cert-$name.pem already exists, skipping"
        return
    fi
    openssl req -x509 -newkey ec \
        -pkeyopt ec_paramgen_curve:P-256 \
        -nodes \
        -days 30 \
        -subj "/CN=$cn" \
        -addext "subjectAltName=DNS:$cn" \
        -keyout "$OUT/key-$name.pem" \
        -out "$OUT/cert-$name.pem" 2>/dev/null
    chmod 600 "$OUT/key-$name.pem"
    echo "  generated cert-$name.pem (CN=$cn)"
}

gen a test-a.local
gen b test-b.local

echo ""
echo "cert-a fingerprint: $(openssl x509 -in "$OUT/cert-a.pem" -noout -fingerprint -sha256 | sed 's/^.*=//')"
echo "cert-b fingerprint: $(openssl x509 -in "$OUT/cert-b.pem" -noout -fingerprint -sha256 | sed 's/^.*=//')"
