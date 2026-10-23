# ATM Guest VM Setup Plan: Local QEMU/KVM Ubuntu 24.04 Environment

> **For agentic workers:** REQUIRED SUB-SKILL: use subagent-orchestration (or superpowers:subagent-driven-development) to implement this plan task by task. Steps use checkbox (`- [ ]`) syntax. **This plan is a planning document only. It does not lift the Phase 1A quarantine, create VMs, generate keys, download images, modify global SSH configurations, or start services.**

**Date:** 2026-10-14  
**Status:** DRAFT. Pending operator review and explicit stage-by-stage authorization.  
**Target Guest Hostname:** `atm-guest`  
**Guest Linux User:** `opencode` (UID/GID 1000, unprivileged: no `sudo`, no `docker` group, locked password)  
**Guest Workspace:** `/home/opencode/agent-task-manager`  
**Host Workdir / Dedicated Directory:** `~/.local/share/atm-guest` (permissions `0700`)  

---

## 1. Goal, Architecture, and Technical Specification

### 1.1 Goal
Establish a self-contained, reproducible, non-production local QEMU/KVM virtual machine (`atm-guest`) to serve as an isolated execution environment for Agent Task Manager (ATM) guest qualification and dirty worktree testing, strictly quarantined from the host development environment.

### 1.2 Architecture
```
+-----------------------------------------------------------------------------------+
| Host System (Linux x86_64, /dev/kvm RW)                                           |
|                                                                                   |
|  ~/.local/share/atm-guest/ (mode 0700)                                            |
|    ├── id_ed25519 / id_ed25519.pub (mode 0600/0644, dedicated, newly generated)  |
|    ├── known_hosts              (mode 0600, pinned guest ed25519 host key)        |
|    ├── ssh_config               (mode 0600, dedicated config for ssh -F)         |
|    ├── atm-guest.qcow2          (24Gi sparse overlay / base snapshot)             |
|    ├── seed.iso                 (NoCloud user-data & meta-data)                   |
|    ├── serial.log               (persisted console output for host-key proof)     |
|    ├── qmp.sock                 (mode 0600, UNIX domain socket for QMP commands)  |
|    └── atm-guest.pid            (tracked QEMU PID)                                |
|                                                                                   |
|  SSH Invocations:                                                                 |
|    ssh -F ~/.local/share/atm-guest/ssh_config atm-guest (127.0.0.1:2222)           |
|                                                                                   |
|  Source Sync:                                                                     |
|    rsync via dedicated SSH into /home/opencode/agent-task-manager                  |
|    (pre-screened snapshot manifest; excludes .git, .no-links, .env, keys, socks)  |
+-----------------------------------------------------------------------------------+
                                         │ Loopback TCP only (127.0.0.1:2222 -> :22)
                                         ▼
+-----------------------------------------------------------------------------------+
| QEMU/KVM Guest (atm-guest, Ubuntu 24.04 LTS Noble Numbat)                         |
|  vCPU: 2 | RAM: 3GiB | CPU Model: -cpu host | Disk: 24GiB                        |
|                                                                                   |
|  Network Phases:                                                                  |
|    Phase A (Bootstrap - Explicit User Approval Required):                         |
|      -netdev user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22                         |
|      *Warning: user-mode NAT can egress to host private IP 10.0.2.2 & LAN.        |
|    Phase B (Sealed / Offline Verification):                                       |
|      -netdev user,id=net0,restrict=on,hostfwd=tcp:127.0.0.1:2222-:22             |
|      *QEMU isolates guest IP routing while retaining explicit hostfwd loopback SSH|
|      OR -nic none with serial console if loopback egress checks fail.             |
|                                                                                   |
|  Guest User: opencode (UID 1000)                                                  |
|    - Login: Dedicated Ed25519 public key only                                     |
|    - Sudo: NONE (sudo: false, locked password)                                    |
|    - Docker / Podman Socket: NONE mounted from host                               |
|    - Model API Auth / Host Environment: NONE shared                               |
|    - Default ubuntu account: locked & deactivated                                 |
+-----------------------------------------------------------------------------------+
```

### 1.3 Technical Specifications & Host Constraints
- **Host Tools Confirmed Present:** `qemu-system-x86_64`, `qemu-img`, `ssh`, `ssh-keygen`, `rsync`, `socat`.
- **Seed ISO Generation Gate:** `cloud-localds`, `genisoimage`, `mkisofs`, `xorriso` are currently **absent** from the host.
  - *Constraint:* Zero silent dependency installation. Seed creation requires explicit operator approval to install `genisoimage` (via `sudo apt-get install -y genisoimage`) or provide an operator-approved ISO generation mechanism.
- **Memory Headroom Pre-flight:** Host has ~5.1–5.6 GiB available with ~1.7 GiB swap used. Guest defaults to 3 GiB RAM. Before boot, host available memory must be verified to exceed 3.5 GiB headroom (`MemAvailable >= 3500000 kB`).
- **CPU Flag Parity:** Uses `-cpu host` with `-enable-kvm` to ensure support for modern instruction set extensions (e.g. AVX2 / Bun JIT features).
- **Network Boundaries:**
  - Loopback binding strictly on `127.0.0.1:2222` (no `0.0.0.0` exposure).
  - No host `sudo`, no host bridge, no TAP devices, and no `NET_ADMIN` capabilities used or requested.
- **Controlled Lifecycle Management:**
  - Guest user `opencode` has no `sudo` privileges. Controlled graceful ACPI powerdown is performed from the host using the QEMU Monitor Protocol (QMP) over an unprivileged UNIX domain socket (`qmp.sock`) via `socat` (`system_powerdown`), with verified PID matching.
- **Phase 1A Quarantine State:** Unconditional quarantine of runtime execution in the ATM repository remains intact (`internal/runner/project_sandbox_manager.go:73-75`). This guest setup provides an isolated playground and does not alter host repository safety guards.

---

## 2. Threat Model & Security Review Focus

| # | Threat | Planned Defense / Control |
|---|---|---|
| **T1** | **Supply-Chain Compromise via Untrusted VM Image** | Cloud image provenance verification requires downloading the official Ubuntu 24.04 cloud image (`noble-server-cloudimg-amd64.img`), fetching `SHA256SUMS` and `SHA256SUMS.gpg`, and verifying the signature using the official Ubuntu Image Signing Service full key fingerprint (`8439 38DF 228D 22F7 B374  2BC0 D94A A3F0 EFE2 1092` / `1A5D 6C4C 7029 FE33 B10F  82C4 8E3A 468D CD57 104B`). Do NOT trust or copy unverified short key IDs. Image verification must succeed before `qemu-img` invocation. |
| **T2** | **Host Key MITM / Blind Key Acceptance** | Prohibit `StrictHostKeyChecking=no` and `UserKnownHostsFile=/dev/null`. Cloud-init generates guest SSH host keys on first boot and prints an explicit ED25519 host public key sentinel line directly to the serial console log (`serial.log`). Host extracts and operator confirms the fingerprint before writing `[127.0.0.1]:2222 <ed25519-public-key>` into `~/.local/share/atm-guest/known_hosts`. |
| **T3** | **Credential Leakage from Host into Guest** | The SSH key `~/.local/share/atm-guest/id_ed25519` is generated fresh exclusively for this guest; existing keys (`~/.ssh/id_*`) are never inspected or imported. `ForwardAgent no` is enforced in `ssh_config`. Model API keys (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`), `.git` files/directories, and `.env*` files are explicitly excluded from source sync. Symlink traversal across boundary is prevented with `--no-links`. |
| **T4** | **Unconfined Guest Network Egress during Testing** | QEMU user-mode networking allows egress to the internet and to host-adjacent private services (`10.0.2.2`). Phase B switches the network to `restrict=on`, which disables external and host routing while preserving the explicit `hostfwd=tcp:127.0.0.1:2222-:22`. Negative network assertions verify that `nc` to `10.0.2.2` and external DNS/HTTP fail closed from within the guest. |
| **T5** | **Process Runaway & State Pollution** | QEMU runs under the unprivileged host user `ghug` with PID written to `~/.local/share/atm-guest/atm-guest.pid`. Teardown uses QMP `system_powerdown` or targeted `SIGTERM` to the validated PID/commandline. No broad `pkill qemu` or reckless cleanup of user directories. |

---

## 3. Implementation Tasks

### Task 1: Dedicated Host Directory, Keypair, Cloud-Init Configuration & Image Verification

- [ ] **1.1 Directory Initialization & Permissions**
  - Verify `~/.local/share/atm-guest` does not exist.
  - Create directory with mode `0700`:
    ```bash
    mkdir -p -m 0700 ~/.local/share/atm-guest
    ```

- [ ] **1.2 Dedicated Ed25519 Keypair Generation**
  - Fail closed if key already exists.
  - Generate new dedicated ed25519 key without passphrase for automated orchestration:
    ```bash
    ssh-keygen -t ed25519 -N "" -C "atm-guest-opencode" -f ~/.local/share/atm-guest/id_ed25519
    chmod 0600 ~/.local/share/atm-guest/id_ed25519
    chmod 0644 ~/.local/share/atm-guest/id_ed25519.pub
    ```

- [ ] **1.3 Dedicated SSH Configuration**
  - Create `~/.local/share/atm-guest/ssh_config` (mode `0600`):
    ```ssh-config
    Host atm-guest
        HostName 127.0.0.1
        Port 2222
        User opencode
        IdentityFile ~/.local/share/atm-guest/id_ed25519
        IdentitiesOnly yes
        UserKnownHostsFile ~/.local/share/atm-guest/known_hosts
        StrictHostKeyChecking yes
        ForwardAgent no
        PasswordAuthentication no
        ConnectTimeout 5
    ```

- [ ] **1.4 Cloud-Init Seed Configuration**
  - Read the generated dedicated public key into an environment variable (validating non-empty):
    ```bash
    PUBKEY=$(cat ~/.local/share/atm-guest/id_ed25519.pub)
    test -n "$PUBKEY" || { echo "Empty public key"; exit 1; }
    ```
  - Create `~/.local/share/atm-guest/user-data` defining unprivileged user `opencode`, locking passwords, disabling root SSH login, and writing the ed25519 host key sentinel to serial console:
    ```yaml
    #cloud-config
    users:
      - default: false
      - name: opencode
        gecos: OpenCode Runtime User
        shell: /bin/bash
        sudo: false
        lock_passwd: true
        ssh_authorized_keys:
          - ${PUBKEY}
    ssh_pwauth: false
    disable_root: true
    bootcmd:
      - [ passwd, -l, ubuntu ]
    runcmd:
      - |
        HOSTKEY=$(cat /etc/ssh/ssh_host_ed25519_key.pub)
        FPRINT=$(ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub | awk '{print $2}')
        echo "=== ATM_GUEST_HOST_ED25519_PUB: ${HOSTKEY} ===" > /dev/console
        echo "=== ATM_GUEST_HOST_ED25519_FPRINT: ${FPRINT} ===" > /dev/console
    ```
  - Create `~/.local/share/atm-guest/meta-data`:
    ```yaml
    instance-id: atm-guest-01
    local-hostname: atm-guest
    ```
  - **Prerequisite Gate Check:** Require `genisoimage` (or operator approved tool). If absent, halt and request operator approval before running:
    ```bash
    genisoimage -output ~/.local/share/atm-guest/seed.iso -volid cidata -joliet -rock \
      ~/.local/share/atm-guest/user-data ~/.local/share/atm-guest/meta-data
    ```

- [ ] **1.5 Ubuntu 24.04 Cloud Image Procurement & Provenance Check**
  - Download official Noble Numbat cloud image, checksums, and signatures:
    ```bash
    TARGET_DIR=~/.local/share/atm-guest
    curl -fsSL -o "$TARGET_DIR/SHA256SUMS" https://cloud-images.ubuntu.com/noble/current/SHA256SUMS
    curl -fsSL -o "$TARGET_DIR/SHA256SUMS.gpg" https://cloud-images.ubuntu.com/noble/current/SHA256SUMS.gpg
    ```
  - Verify GPG signature against Ubuntu Image Signing Service full fingerprint:
    - Expected fingerprint: `8439 38DF 228D 22F7 B374  2BC0 D94A A3F0 EFE2 1092` / `1A5D 6C4C 7029 FE33 B10F  82C4 8E3A 468D CD57 104B`.
    - Validate `gpg --verify $TARGET_DIR/SHA256SUMS.gpg $TARGET_DIR/SHA256SUMS`.
  - Download `noble-server-cloudimg-amd64.img` and verify:
    ```bash
    grep "noble-server-cloudimg-amd64.img" "$TARGET_DIR/SHA256SUMS" | (cd "$TARGET_DIR" && sha256sum -c)
    ```
  - Create 24GiB sparse QCOW2 guest overlay:
    ```bash
    qemu-img create -f qcow2 -b "$TARGET_DIR/noble-server-cloudimg-amd64.img" -F qcow2 "$TARGET_DIR/atm-guest.qcow2" 24G
    ```

---

### Task 2: Secure VM Lifecycle, Network Confinement & Snapshot Sync

- [ ] **2.1 Pre-boot Memory & Port Validation**
  - Check host memory available: ensure `MemAvailable >= 3500000 kB` via `/proc/meminfo`.
  - Check loopback port `127.0.0.1:2222` is free (`ss -tulpn | grep 2222`).

- [ ] **2.2 Phase A: Initial Bootstrap Launch (Operator Network Consent Required)**
  - *Risk Notice:* Phase A enables user-mode network egress solely for cloud-init initial bootstrapping if package installs are needed. Operator must explicitly consent before running this command.
  - Launch with persisted serial console output and QMP control socket:
    ```bash
    qemu-system-x86_64 \
      -name atm-guest,process=atm-guest \
      -machine q35,accel=kvm \
      -cpu host \
      -smp 2 \
      -m 3072 \
      -drive file=~/.local/share/atm-guest/atm-guest.qcow2,format=qcow2,if=virtio \
      -cdrom ~/.local/share/atm-guest/seed.iso \
      -netdev user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22 \
      -device virtio-net-pci,netdev=net0 \
      -qmp unix:~/.local/share/atm-guest/qmp.sock,server=on,wait=off \
      -serial file:~/.local/share/atm-guest/serial.log \
      -display none \
      -pidfile ~/.local/share/atm-guest/atm-guest.pid \
      -daemonize
    ```

- [ ] **2.3 Host Key Extraction and Verification (No Blind sshkeyscan / accept-new)**
  - Monitor `serial.log` for the boot completion sentinel:
    ```bash
    grep "=== ATM_GUEST_HOST_ED25519_PUB:" ~/.local/share/atm-guest/serial.log
    ```
  - Extract guest host public key:
    ```bash
    PUBKEY_LINE=$(grep "=== ATM_GUEST_HOST_ED25519_PUB:" ~/.local/share/atm-guest/serial.log | sed 's/=== ATM_GUEST_HOST_ED25519_PUB: //;s/ ===//')
    FPRINT_LINE=$(grep "=== ATM_GUEST_HOST_ED25519_FPRINT:" ~/.local/share/atm-guest/serial.log | sed 's/=== ATM_GUEST_HOST_ED25519_FPRINT: //;s/ ===//')
    echo "Extracted Host Key Fingerprint: $FPRINT_LINE"
    ```
  - Write verified host key into dedicated `known_hosts` (mode `0600`):
    ```bash
    echo "[127.0.0.1]:2222 $PUBKEY_LINE" > ~/.local/share/atm-guest/known_hosts
    chmod 0600 ~/.local/share/atm-guest/known_hosts
    ```
  - Verify initial unprivileged SSH connectivity:
    ```bash
    ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "echo ssh-connected"
    ```

- [ ] **2.4 Host-Controlled Graceful Shutdown via QMP**
  - Note: User `opencode` has no `sudo` privileges. Shutdown is triggered host-side via QMP `system_powerdown` using existing host utility `socat`:
    ```bash
    # QMP handshake: read greeting, negotiate capabilities, issue ACPI powerdown
    (
      sleep 0.2
      echo '{"execute": "qmp_capabilities"}'
      sleep 0.2
      echo '{"execute": "system_powerdown"}'
      sleep 0.2
    ) | socat - UNIX-CONNECT:~/.local/share/atm-guest/qmp.sock
    ```
  - Wait up to 30 seconds for QEMU process termination:
    ```bash
    PID=$(cat ~/.local/share/atm-guest/atm-guest.pid 2>/dev/null)
    for i in $(seq 1 30); do
        kill -0 "$PID" 2>/dev/null || break
        sleep 1
    done
    if kill -0 "$PID" 2>/dev/null; then
        echo "ACPI powerdown timeout; falling back to SIGTERM on PID $PID"
        kill -TERM "$PID"
    fi
    rm -f ~/.local/share/atm-guest/qmp.sock
    ```

- [ ] **2.5 Phase B: Sealed / Offline Launch with Restricted Loopback SSH**
  - Launch guest with `restrict=on` to block guest-to-host and guest-to-internet egress:
    ```bash
    qemu-system-x86_64 \
      -name atm-guest,process=atm-guest \
      -machine q35,accel=kvm \
      -cpu host \
      -smp 2 \
      -m 3072 \
      -drive file=~/.local/share/atm-guest/atm-guest.qcow2,format=qcow2,if=virtio \
      -netdev user,id=net0,restrict=on,hostfwd=tcp:127.0.0.1:2222-:22 \
      -device virtio-net-pci,netdev=net0 \
      -qmp unix:~/.local/share/atm-guest/qmp.sock,server=on,wait=off \
      -serial file:~/.local/share/atm-guest/serial.log \
      -display none \
      -pidfile ~/.local/share/atm-guest/atm-guest.pid \
      -daemonize
    ```
  - **Negative Egress Verification (Fail Closed):**
    - Verify host SSH connectivity succeeds:
      ```bash
      ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "echo guest-alive"
      ```
    - Verify guest egress to host gateway (`10.0.2.2`) is blocked:
      ```bash
      ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "nc -z -w 2 10.0.2.2 22 || echo egress-blocked"
      ```
    - Verify guest egress to external network (`8.8.8.8` / public DNS) fails:
      ```bash
      ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "ping -c 1 -W 2 8.8.8.8 || echo ping-blocked"
      ```
    - *Contingency:* If `restrict=on` allows any non-forwarded outbound traffic in the test, immediately terminate QEMU and switch to `-nic none` with serial console automation.

- [ ] **2.6 Dirty Worktree Snapshot Transfer**
  - **Boundary Guarantee & Symlink Defense:**
    - Linked worktrees contain a `.git` regular file (e.g., `gitdir: ...`), not a directory. Using `--exclude='.git/'` leaks this file. Using `--exclude='.git'` matches both files and directories at any path depth.
    - Transfer disables symlink following and symlink replication across boundaries using `--no-links`.
  - **Pre-transfer Manifest Generation & Review:**
    ```bash
    rsync -avn --no-links --exclude='.git' --exclude='.env*' --exclude='*.key' --exclude='*.pem' \
      --exclude='*.sock' --exclude='node_modules' \
      ./ atm-guest:/home/opencode/agent-task-manager/ > ~/.local/share/atm-guest/transfer-manifest.txt
    ```
  - Operator reviews `~/.local/share/atm-guest/transfer-manifest.txt`.
  - **Negative Test Fixtures Verification (Future Implementation Gate):**
    - Test that a fixture `.git` regular file and an off-root symlink fixture are completely rejected and omitted from the manifest before transfer execution.
  - Execute transfer:
    ```bash
    rsync -avz --no-links --exclude='.git' --exclude='.env*' --exclude='*.key' --exclude='*.pem' \
      --exclude='*.sock' --exclude='node_modules' \
      -e "ssh -F $HOME/.local/share/atm-guest/ssh_config" \
      ./ atm-guest:/home/opencode/agent-task-manager/
    ```
  - Verify target directory ownership is unprivileged `opencode:opencode`:
    ```bash
    ssh -F ~/.local/share/atm-guest/ssh_config atm-guest "ls -ld /home/opencode/agent-task-manager"
    ```

- [ ] **2.7 Controlled Teardown Procedure**
  - Verify running QEMU PID matches recorded `atm-guest.pid` and commandline:
    ```bash
    PID=$(cat ~/.local/share/atm-guest/atm-guest.pid 2>/dev/null)
    if [ -n "$PID" ] && ps -p "$PID" -o cmd= | grep -q "qemu-system-x86_64.*atm-guest"; then
        (
          echo '{"execute": "qmp_capabilities"}'
          sleep 0.2
          echo '{"execute": "system_powerdown"}'
        ) | socat - UNIX-CONNECT:~/.local/share/atm-guest/qmp.sock 2>/dev/null || kill -TERM "$PID"
    fi
    ```

---

## 4. Acceptance Criteria & Operator Review Gate

### Acceptance Criteria
1. **Host Isolation:** No global SSH config changes (`~/.ssh/config` untouched); all operations use `ssh -F ~/.local/share/atm-guest/ssh_config`.
2. **Key Security:** Dedicated `id_ed25519` keypair generated in `~/.local/share/atm-guest` with `0600`/`0644` permissions; host keys pinned to dedicated `known_hosts` without `StrictHostKeyChecking=no`.
3. **Provenance Integrity:** Ubuntu 24.04 cloud image cryptographically validated using full GPG key fingerprint and `sha256sum -c`.
4. **Network Confinement:** In Phase B, guest outbound traffic to `10.0.2.2` and the public internet is demonstrably dropped (`restrict=on`), while loopback SSH forwarding on `127.0.0.1:2222` functions reliably.
5. **Runtime Quarantine Maintained:** ATM repository quarantine is not modified or bypassed. No Docker socket is mounted into the guest.

---

## 5. Explicit Operator Review & Approval Request

Execution of this plan is held pending explicit operator approval on the following decisions:
1. **Missing Seed ISO Tool:** `cloud-localds`, `genisoimage`, `mkisofs`, and `xorriso` are currently absent on the host. Operator approval is required to install `genisoimage` (via `sudo apt-get install genisoimage`) or specify an alternative cloud-init delivery mechanism.
2. **Phase A Bootstrap Egress Authorization:** Confirmation to temporarily allow QEMU user NAT network access during the initial cloud-init guest bootstrap phase prior to switching to restricted mode.
3. **Approval to Proceed with Task 1 Implementation:** Operator authorization to generate dedicated keys and download the verified Ubuntu cloud image into `~/.local/share/atm-guest`.
