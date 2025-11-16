#!/usr/bin/env bash
# Bring up DHCP on any available non-lo interface (Debian-friendly).
# Usage:
#   sudo ./bringup-dhcp.sh            # one-shot DHCP
#   sudo ./bringup-dhcp.sh --persist  # also write /etc/network/interfaces.d/01-<iface>-dhcp

set -euo pipefail

persist=0
forced_if=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --persist) persist=1; shift;;
    --iface)   forced_if="${2:-}"; shift 2;;
    -h|--help) echo "Usage: $0 [--persist] [--iface IFACE]"; exit 0;;
    *) echo "Unknown arg: $1" >&2; exit 2;;
  esac
done

need_root() { [[ $EUID -eq 0 ]] || { echo "Run as root." >&2; exit 1; }; }
need_root

# Ensure a DHCP client exists (Debian default: isc-dhcp-client)
if ! command -v dhclient >/dev/null 2>&1; then
  echo "dhclient not found. Installing isc-dhcp-client..." >&2
  apt-get update -y && apt-get install -y isc-dhcp-client
fi

pick_iface() {
  local ifs=()
  if [[ -n "$forced_if" ]]; then
    echo "$forced_if"; return
  fi

  # Gather candidate NICs: non-lo, has a device, avoid obvious virtuals we don't want
  while IFS= read -r d; do
    local n="${d##*/}"
    [[ "$n" == "lo" ]] && continue
    [[ -e "$d/device" ]] || continue
    [[ "$n" =~ ^(docker|veth|br|virbr|tun|tap)$ ]] && continue
    ifs+=("$n")
  done < <(ls -1d /sys/class/net/*)

  # 1) prefer carrier
  for n in "${ifs[@]}"; do
    ip link set "$n" up || true
    [[ -r "/sys/class/net/$n/carrier" ]] && [[ "$(cat "/sys/class/net/$n/carrier")" == "1" ]] && { echo "$n"; return; }
  done
  # 2) otherwise first candidate
  [[ ${#ifs[@]} -gt 0 ]] && { echo "${ifs[0]}"; return; }

  echo ""
}

IFACE="$(pick_iface)"
[[ -n "$IFACE" ]] || { echo "No usable interface found." >&2; exit 1; }

# Bring link up
ip link set "$IFACE" up || true

# Kill any previous dhclient for this iface (idempotent runs)
pkill -f "dhclient.*\b$IFACE\b" >/dev/null 2>&1 || true

# Use interface-specific pid/lease files to avoid collisions
PIDF="/run/dhclient.$IFACE.pid"
LEASEF="/var/lib/dhcp/dhclient.$IFACE.leases"
mkdir -p /var/lib/dhcp

echo "Attempting DHCP on $IFACE…"
if timeout 25s dhclient -1 -4 -v -pf "$PIDF" -lf "$LEASEF" "$IFACE"; then
  IP4=$(ip -4 -o addr show dev "$IFACE" | awk '{print $4}')
  GW=$(ip route show dev "$IFACE" default | awk '/default/{print $3}')
  echo "Success: $IFACE -> $IP4  gw=${GW:-<none>}"

  if ((persist)); then
    install -d -m 0755 /etc/network/interfaces.d
    CFG="/etc/network/interfaces.d/01-$IFACE-dhcp"
    cat >"$CFG" <<CFGEOF
auto $IFACE
iface $IFACE inet dhcp
CFGEOF
    echo "Persisted DHCP config to $CFG"
  fi
  exit 0
else
  echo "DHCP failed on $IFACE." >&2
  exit 1
fi
