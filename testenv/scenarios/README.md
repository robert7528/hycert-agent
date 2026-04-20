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
Port:                8445           # nginx-mismatch: cert-a pub + key-b private
ExpectedFingerprint: $FP_A
```

Note: on nginx versions that validate cert/key at startup,
`nginx-mismatch` may fail to start — that also satisfies the scenario
(port is bound by nothing → probe gets ConnRefused, not
HandshakeFailure). To force HandshakeFailure specifically, use an
nginx version older than the validating one, or swap to a service
that loads cert/key lazily (haproxy serves this purpose on many
distros).
