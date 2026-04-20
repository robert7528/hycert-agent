# Manual probe scenarios

After `make up` in the parent directory, exercise each VerifyResult
state by running `ProbeTLS` directly (or via a deployment target
configured against the local testenv ports).

## Quick probe script

```bash
# Get the fingerprints
FP_A=$(make -s -C .. fingerprint-a)
FP_B=$(make -s -C .. fingerprint-b)

cat <<EOF
cert-a fingerprint: $FP_A
cert-b fingerprint: $FP_B
EOF
```

## Scenario 1 — Match

Expected: `ResultMatch`

```
Host:                127.0.0.1
Port:                8443
ExpectedFingerprint: $FP_A
SNIList:             [test-a.local]
```

## Scenario 2 — Mismatch

Expected: `ResultMismatch` (error-level)

```
Host:                127.0.0.1
Port:                8444
ExpectedFingerprint: $FP_A        # agent deployed cert-a …
SNIList:             [test-a.local]
# … but 8444 is serving cert-b
```

## Scenario 3 — ConnRefused

Expected: `ResultConnRefused` (warning-level)

```
Host:                127.0.0.1
Port:                9999           # nothing listening
ExpectedFingerprint: $FP_A
```

## Scenario 4 — HandshakeFailure

Expected: `ResultHandshakeFailure` (error-level, v0.3.1+)

```
Host:                127.0.0.1
Port:                8445           # tls-reject helper: closes TCP pre-handshake
ExpectedFingerprint: $FP_A
```

`tls-reject` is a small Go helper (source: `../bin/tls-reject/main.go`)
that accepts TCP connections and immediately closes them before any
TLS bytes are exchanged. From the client's point of view: TCP connect
succeeds → TLS handshake reads EOF → Go surfaces a handshake error →
ProbeTLS classifies as `ResultHandshakeFailure`.

Why not use nginx with client-cert requirement? We tried. Modern
nginx's `ssl_verify_client on` completes the TLS handshake and only
rejects at the HTTP layer (response 400 "No required SSL certificate
was sent"). ProbeTLS considers the TLS handshake successful and reads
the server cert — classification becomes Match, not HandshakeFailure.
The Go helper bypasses nginx behaviour and forces a real handshake-
layer failure, which is what real-world cert/key mismatch incidents
look like to a TLS client.
