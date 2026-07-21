#!/usr/bin/env bash
# ci/vm/systemd/poc.sh — Gate 5: systemd proof of concept inside a booted
# system VM. Requires systemd as PID 1. Run by vm-run.sh / manually via:
#   docker exec <system-vm> /opt/ci/systemd/poc.sh
set -euo pipefail

echo "[poc] pid1=$(cat /proc/1/comm)"
[ "$(cat /proc/1/comm)" = "systemd" ] || { echo "[poc] FAIL: PID 1 is not systemd" >&2; exit 1; }

echo "[poc] system state: $(systemctl is-system-running 2>&1)"
systemctl is-system-running 2>&1 | grep -Eq 'running|degraded'

# D-Bus is exercised implicitly by systemctl (it talks to systemd over D-Bus);
# verify the bus explicitly as well.
dbus-send --system --dest=org.freedesktop.DBus --type=method_call --print-reply \
  / org.freedesktop.DBus.ListNames >/dev/null
echo "[poc] D-Bus ok"

install -m 0644 /opt/ci/systemd/veil-ci-test.service /etc/systemd/system/
install -m 0644 /opt/ci/systemd/veil-ci-echo.service /etc/systemd/system/
install -m 0644 /opt/ci/systemd/veil-ci-echo.socket /etc/systemd/system/
systemctl daemon-reload

systemctl start veil-ci-test.service
systemctl is-active veil-ci-test.service | grep -qx active
[ "$(cat /run/veil-ci-test.marker)" = "veil-ci-test-service-alive" ]
echo "[poc] test service ok"

# Socket activation: the .service must be started by systemd on the first
# connection to the socket (curl's protocol error is fine — the connect itself
# is the trigger).
systemctl start veil-ci-echo.socket
systemctl is-active veil-ci-echo.socket | grep -qx active
! systemctl is-active veil-ci-echo.service >/dev/null 2>&1
# Trigger activation: the connect itself starts the service (curl exits with a
# protocol error since nothing answers — that is expected and fine).
curl --silent --max-time 5 --unix-socket /run/veil-ci-echo.sock http://localhost/ >/dev/null 2>&1 || true
for _ in $(seq 1 10); do
  systemctl is-active veil-ci-echo.service >/dev/null 2>&1 && break
  sleep 1
done
systemctl is-active veil-ci-echo.service >/dev/null 2>&1
systemctl show veil-ci-echo.service -p SubState | grep -qx 'SubState=exited'
echo "[poc] socket activation ok"

journalctl -u veil-ci-test.service --no-pager -n 5
echo "[poc] journal ok"

systemctl stop veil-ci-echo.service veil-ci-echo.socket veil-ci-test.service
rm -f /etc/systemd/system/veil-ci-*.service /etc/systemd/system/veil-ci-*.socket /run/veil-ci-test.marker
systemctl daemon-reload
echo "[poc] PASS"
