# Operator egress firewall (Gate 0D)

Host-operator files; ATM does not apply, edit or persist them. Set `WAN_IFACE` in `atm.nft`, adjust the pool, proxy
and gateway IPs, then:

    install -D atm.nft /etc/atm/atm.nft
    install -m755 atm-fw-apply.sh /usr/local/sbin/
    install atm-fw.service /etc/systemd/system/ && systemctl enable --now atm-fw

`qualify/` holds the canary listeners and the matrix run in the VM test. Run them on a disposable VM only. The test
fixture addresses (198.51.100.50, 172.28.0.2 as a dummy interface) are not part of a production setup.
Read section 9 of `docs/superpowers/plans/2026-10-16-gate-0d-operator-handoff.md` before relying on any of this.
