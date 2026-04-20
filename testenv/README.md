# hycert-agent integration test environment

Local podman-based harness for exercising `verify.ProbeTLS` against
real TLS services in each of the classification states without
touching any customer environment.

## Prereqs

* podman 4+ (tested on 5.6.0 rootful, SELinux disabled)
* openssl 3+ (for cert generation)
* ports 8443 / 8444 / 8445 free on the host

## Scenarios

| # | Scenario         | Setup                                       | Expected VerifyResult   |
|---|------------------|---------------------------------------------|-------------------------|
| 1 | Match            | nginx-a (8443) serves cert-a; deploy cert-a | `match`                 |
| 2 | Mismatch         | nginx-b (8444) serves cert-b; deploy cert-a | `mismatch`              |
| 3 | ConnRefused      | probe port 9999 (no service)                | `conn_refused`          |
| 4 | HandshakeFailure | nginx-mismatch (8445) requires client cert  | `handshake_failure`     |

`timeout` classification is not exercised end-to-end — it requires
mid-probe state changes that are difficult to reproduce reliably with
static containers. Unit test `TestClassify_Mixed` in `verify/tls_test.go`
covers the logic.

## Usage

```bash
make certs      # one-time: generate cert-a, cert-b, wrongkey pair
make up         # start all three nginx containers
make verify     # quick sanity: each port returns the expected fingerprint
make down       # stop + remove containers
```

After `make up`, run the agent against each scenario manually — see
`scenarios/` for ready-to-use target_detail JSON snippets.

## Cleanup

`make down` removes containers but leaves generated certs under `certs/`.
`make clean` removes certs too.
