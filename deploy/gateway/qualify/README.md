# Gate 0G qualification fixtures

Disposable-VM fixtures used to run the spec §10 matrix (0G-PROBE-01..18). Not for production.

- `gateway-qualify.go.txt`: harness source (kept as `.txt` so it is not built or scanned). Copy to a throwaway
  `cmd/` dir to build. Subcommands: `certs`, `upstream` (fake TLS provider on :9443, redirect target on :9444),
  `config` (real `gateway.OpenCodeConfig`, https), `gateway` (real `ServiceFromEnv`/`Run` plus a loopback-only admin
  endpoint on 127.0.0.1:9100 that mints/releases tokens).
- `start.sh`: certs, CA trust, fake upstream, gateway. `gateway-probes.sh`: probes 06-15, 18 from a sandbox-network
  container. `container-probes.sh`: probes 01-05, 16, 17 and the OpenCode end-to-end/task-child checks against a
  container created like the (quarantined) project sandbox draft. `Dockerfile.sandbox`: the project template minus toolchains.
- The gateway was run with ATM_GATEWAY_FIREWALL_ATTESTATION on the Gate 0D firewall VM, listening on 172.29.0.2:8082.
- TLS and spend (B3/B4): `certs` also writes a separate gateway CA and an IP-SAN leaf for 172.29.0.2. `start.sh` serves TLS 1.3 with a price file and `ATM_GATEWAY_QUOTA_USD`; probes use `--cacert`. `Dockerfile.sandbox` adds the CA like `Dockerfile.project.template` (the VM image was derived with plain COPY because it has no BuildKit). `start-spend.sh` + `spend-probes.sh` run a replacement gateway with a tiny dollar cap on :8082 (the firewall admits only that port); `GWQ_MAX_BODY` raises the body cap for the OpenCode run, whose requests are ~22 KB. Run `container-probes.sh` with `IMG=atm-0g-sandbox` for the no-CA negative control.
