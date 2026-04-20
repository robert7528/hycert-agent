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

.PHONY: certs up down clean verify fingerprint-a fingerprint-b

## generate cert-a, cert-b, key pair mismatch material (idempotent)
certs:
	@mkdir -p $(HERE)/certs
	@bash $(HERE)/gen-certs.sh $(HERE)/certs

## start all three test nginx containers
up: certs
	@podman rm -f $(PREFIX)-nginx-a $(PREFIX)-nginx-b $(PREFIX)-nginx-mismatch 2>/dev/null || true
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
	@echo "=== nginx-mismatch on 8445 (mTLS required — clients without cert get handshake failure) ==="
	podman run -d --name $(PREFIX)-nginx-mismatch \
	    -p 127.0.0.1:8445:443 \
	    -v $(HERE)/nginx/nginx-mismatch.conf:/etc/nginx/conf.d/default.conf:ro \
	    -v $(HERE)/certs/cert-a.pem:/etc/nginx/ssl/cert.pem:ro \
	    -v $(HERE)/certs/key-a.pem:/etc/nginx/ssl/key.pem:ro \
	    -v $(HERE)/certs/cert-a.pem:/etc/nginx/ssl/ca.pem:ro \
	    $(IMAGE)
	@sleep 1
	@$(MAKE) --no-print-directory verify

## stop and remove all test containers
down:
	-podman rm -f $(PREFIX)-nginx-a $(PREFIX)-nginx-b $(PREFIX)-nginx-mismatch 2>/dev/null

## remove containers AND generated certs
clean: down
	rm -rf $(HERE)/certs

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
	@echo "=== sanity: handshake failure expected on 8445 ==="
	@echo | openssl s_client -connect 127.0.0.1:8445 -servername test-a.local 2>&1 \
	    | grep -E 'handshake|alert|CONNECTED' | head -3 || echo "(no output — that's also possible)"

## print cert-a SHA-256 fingerprint (use as ExpectedFingerprint for scenario 1)
fingerprint-a: certs
	@openssl x509 -in $(HERE)/certs/cert-a.pem -noout -fingerprint -sha256 | sed 's/^.*=//'

## print cert-b SHA-256 fingerprint
fingerprint-b: certs
	@openssl x509 -in $(HERE)/certs/cert-b.pem -noout -fingerprint -sha256 | sed 's/^.*=//'
