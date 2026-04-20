# hycert-agent test environment
#
# Spins up three nginx containers on 127.0.0.1:8443/8444/8445 with
# pre-generated self-signed certs so ProbeTLS can be exercised against
# each VerifyResult state. All containers are prefixed `hycert-testenv-`
# to avoid collision with production containers on the shared host.

SHELL := /bin/bash
PREFIX := hycert-testenv
HERE := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
IMAGE := docker.io/library/nginx:alpine
TLS_REJECT_BIN := $(HERE)/bin/tls-reject.bin
TLS_REJECT_PID := /tmp/hycert-testenv-tls-reject.pid

.PHONY: certs up down clean verify fingerprint-a fingerprint-b tls-reject-up tls-reject-down

## generate cert-a, cert-b, key pair mismatch material (idempotent)
certs:
	@mkdir -p $(HERE)/certs
	@bash $(HERE)/gen-certs.sh $(HERE)/certs

## start all test services (2 nginx containers + tls-reject helper)
up: certs tls-reject-up
	@podman rm -f $(PREFIX)-nginx-a $(PREFIX)-nginx-b 2>/dev/null || true
	@echo "=== nginx-a on 8443 (serves cert-a) ==="
	podman run -d --name $(PREFIX)-nginx-a \
	    -p 127.0.0.1:8443:443 \
	    -v $(HERE)/nginx/nginx-a.conf:/etc/nginx/conf.d/default.conf:ro \
	    -v $(HERE)/certs/cert-a.pem:/etc/nginx/ssl/cert.pem:ro \
	    -v $(HERE)/certs/key-a.pem:/etc/nginx/ssl/key.pem:ro \
	    $(IMAGE)
	@echo "=== nginx-b on 8444 (serves cert-b) ==="
	podman run -d --name $(PREFIX)-nginx-b \
	    -p 127.0.0.1:8444:443 \
	    -v $(HERE)/nginx/nginx-b.conf:/etc/nginx/conf.d/default.conf:ro \
	    -v $(HERE)/certs/cert-b.pem:/etc/nginx/ssl/cert.pem:ro \
	    -v $(HERE)/certs/key-b.pem:/etc/nginx/ssl/key.pem:ro \
	    $(IMAGE)
	@sleep 1
	@$(MAKE) --no-print-directory verify

## stop all test services
down: tls-reject-down
	-podman rm -f $(PREFIX)-nginx-a $(PREFIX)-nginx-b 2>/dev/null

## build + launch tls-reject on 8445 (HandshakeFailure scenario)
tls-reject-up: $(TLS_REJECT_BIN) tls-reject-down
	@echo "=== tls-reject on 8445 (closes connection pre-handshake) ==="
	@$(TLS_REJECT_BIN) 8445 &>/tmp/hycert-testenv-tls-reject.log & echo $$! > $(TLS_REJECT_PID)
	@sleep 0.3
	@if ! kill -0 $$(cat $(TLS_REJECT_PID)) 2>/dev/null; then \
	    echo "tls-reject failed to start; see /tmp/hycert-testenv-tls-reject.log"; \
	    exit 1; \
	fi
	@echo "  pid=$$(cat $(TLS_REJECT_PID))"

## stop tls-reject if running
tls-reject-down:
	@if [[ -f $(TLS_REJECT_PID) ]]; then \
	    pid=$$(cat $(TLS_REJECT_PID)); \
	    kill $$pid 2>/dev/null || true; \
	    rm -f $(TLS_REJECT_PID); \
	fi

## build tls-reject helper binary
$(TLS_REJECT_BIN): $(HERE)/bin/tls-reject/main.go
	@go build -o $@ $(HERE)/bin/tls-reject

## remove containers, tls-reject, and generated certs
clean: down
	rm -rf $(HERE)/certs
	rm -f $(TLS_REJECT_BIN)

## sanity check each port returns the expected cert (or handshake fails for scenario 4)
verify:
	@echo ""
	@echo "=== sanity: cert-a fingerprint on 8443 ==="
	@echo | openssl s_client -connect 127.0.0.1:8443 -servername test-a.local 2>/dev/null \
	    | openssl x509 -noout -subject -fingerprint -sha256 || echo "FAILED"
	@echo ""
	@echo "=== sanity: cert-b fingerprint on 8444 ==="
	@echo | openssl s_client -connect 127.0.0.1:8444 -servername test-b.local 2>/dev/null \
	    | openssl x509 -noout -subject -fingerprint -sha256 || echo "FAILED"
	@echo ""
	@echo "=== sanity: handshake failure expected on 8445 (tls-reject) ==="
	@echo | openssl s_client -connect 127.0.0.1:8445 2>&1 | grep -E 'CONNECTED|error|alert' | head -3 || true

## print cert-a SHA-256 fingerprint (use as ExpectedFingerprint for scenario 1)
##
## NOT dependent on `certs` target — gen-certs.sh emits informational
## echoes that would pollute `make -s fingerprint-a` output used for
## env-var capture. Run `make up` (or `make certs`) first to populate
## certs/; if missing, openssl exits non-zero with a clear error.
fingerprint-a:
	@openssl x509 -in $(HERE)/certs/cert-a.pem -noout -fingerprint -sha256 | sed 's/^.*=//'

## print cert-b SHA-256 fingerprint
fingerprint-b:
	@openssl x509 -in $(HERE)/certs/cert-b.pem -noout -fingerprint -sha256 | sed 's/^.*=//'
