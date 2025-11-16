#!/usr/bin/env bash
set -euo pipefail

err() { printf "Error: %s\n" "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || err "Missing required command: $1"; }
need virsh
need awk
need sed
need grep
need xmllint   # new: used to parse net XML reliably

CLEAR_STATIC=false
CLEAR_ALL=false
REMOVE_ONLY=false

# ---- option parsing ----
ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -c|--clear-static) CLEAR_STATIC=true; shift ;;
    -C|--clear-all)    CLEAR_ALL=true;  shift ;;
    -r|--remove) REMOVE_ONLY=true; shift ;;
    -h|--help)
      cat >&2 <<'USAGE'
Usage:
  pin-dhcp-lease.sh [OPTIONS] <vm-name> <network-name>
  pin-dhcp-lease.sh --clear-all <network-name>

Options:
  -c, --clear-static   Remove all static DHCP host entries, then pin the lease.
  -C, --clear-all      Remove all static DHCP host entries and exit (no pin).
  -r, --remove         Remove the pinned lease for the specified VM/network.

Examples:
  pin-dhcp-lease.sh myvm default
  pin-dhcp-lease.sh -c myvm default
  pin-dhcp-lease.sh -r myvm default
  pin-dhcp-lease.sh --clear-all default
USAGE
      exit 2
      ;;
    --) shift; break ;;
    -*)
      err "Unknown option: $1"
      ;;
    *)
      ARGS+=("$1"); shift ;;
  esac
done
set -- "${ARGS[@]}"

# ---- argument handling ----
VM_NAME=""
NET_NAME=""

if $CLEAR_ALL; then
  # Accept 1 or 2 args; if 2, ignore VM.
  if [[ $# -eq 1 ]]; then
    NET_NAME="$1"
  elif [[ $# -eq 2 ]]; then
    VM_NAME="$1"   # ignored
    NET_NAME="$2"
  else
    err "For --clear-all, provide: pin-dhcp-lease.sh --clear-all <network-name> (or add a VM name which will be ignored)."
  fi
else
  if [[ $# -ne 2 ]]; then
    cat >&2 <<'USAGE'
Usage:
  pin-dhcp-lease.sh [--clear-static|-c] <vm-name> <network-name>

Run with -h for more details.
USAGE
    exit 2
  fi
  VM_NAME="$1"
  NET_NAME="$2"
fi

if $REMOVE_ONLY && $CLEAR_ALL; then
  err "Cannot combine --remove with --clear-all"
fi

if $REMOVE_ONLY && $CLEAR_STATIC; then
  err "Cannot combine --remove with --clear-static"
fi

# Ensure network exists early (works for both clear modes and pin mode)
if ! virsh net-info "$NET_NAME" >/dev/null 2>&1; then
  err "Network '$NET_NAME' not found."
fi

# ---- helpers ----
clear_static_hosts() {
  echo "Removing all static DHCP host entries from '$NET_NAME'..."
  local tmpfile; tmpfile="$(mktemp)"
  virsh net-dumpxml "$NET_NAME" >"$tmpfile"

  # Collect all <host .../> nodes under any <ip><dhcp> (IPv4 or IPv6)
  local hosts
  hosts="$(xmllint --xpath "string-join(//network/ip/dhcp/host, '||SEP||')" "$tmpfile" 2>/dev/null || true)"
  rm -f "$tmpfile"

  if [[ -z "$hosts" ]]; then
    echo "No static DHCP entries found."
    return 0
  fi

  IFS=$'\n' read -r -d '' -a host_array < <(printf '%s\0' "${hosts//||SEP||/$'\n'}")
  for host in "${host_array[@]}"; do
    [[ -z "${host// }" ]] && continue
    # Delete both live and config; ignore failures
    set +e
    virsh net-update "$NET_NAME" delete ip-dhcp-host "$host" --live >/dev/null 2>&1
    virsh net-update "$NET_NAME" delete ip-dhcp-host "$host" --config >/dev/null 2>&1
    set -e
  done
  echo "All static DHCP host entries removed."
}

# ---- clear-all short-circuit ----
if $CLEAR_ALL; then
  clear_static_hosts
  echo "Done. Cleared all static DHCP host entries on '${NET_NAME}'."
  exit 0
fi

# ---- regular flow (optionally pre-clear then pin) ----
# Ensure domain exists
if ! virsh dominfo "$VM_NAME" >/dev/null 2>&1; then
  err "Domain '$VM_NAME' not found."
fi

# Extract the VM's MAC for the specified network.
VM_MAC="$(virsh domiflist "$VM_NAME" \
  | awk -v net="$NET_NAME" 'tolower($3)==tolower(net) {print $5; exit}')"
[[ -n "${VM_MAC:-}" ]] || err "No interface on network '$NET_NAME' found for domain '$VM_NAME'."

delete_host_entry() {
  local mac=$1 ip=${2:-}
  set +e
  virsh net-update "$NET_NAME" delete ip-dhcp-host "<host mac='${mac}'/>" --live >/dev/null 2>&1
  virsh net-update "$NET_NAME" delete ip-dhcp-host "<host mac='${mac}'/>" --config >/dev/null 2>&1
  if [[ -n "${ip:-}" ]]; then
    virsh net-update "$NET_NAME" delete ip-dhcp-host "<host ip='${ip}'/>" --live >/dev/null 2>&1
    virsh net-update "$NET_NAME" delete ip-dhcp-host "<host ip='${ip}'/>" --config >/dev/null 2>&1
  fi
  set -e
}

# Prefer IPv4 lease
LEASE_IP="$(virsh net-dhcp-leases "$NET_NAME" 2>/dev/null \
  | awk -v mac="$VM_MAC" '
      BEGIN{IGNORECASE=1;ipv4_re="^([0-9]{1,3}\\.){3}[0-9]{1,3}(/([0-9]{1,2}))?$"}
      tolower($0) ~ tolower(mac) {
        for(i=1;i<=NF;i++){ if($i ~ ipv4_re){ gsub(/\/[0-9]+$/,"",$i); print $i; exit } }
      }')"
if [[ -z "${LEASE_IP:-}" ]]; then
  echo "Warning: No current DHCP lease found for MAC $VM_MAC on '$NET_NAME'. Proceeding without IP-specific cleanup." >&2
fi

if $REMOVE_ONLY; then
  delete_host_entry "$VM_MAC" "$LEASE_IP"
  echo "Removed pinned lease entries for ${VM_NAME} on '${NET_NAME}'."
  exit 0
fi

[[ -n "${LEASE_IP:-}" ]] || err "No current DHCP lease (IPv4) found for MAC $VM_MAC on '$NET_NAME'. Start the VM or ensure it has a lease."

# Optionally clear static entries before pinning
if $CLEAR_STATIC; then
  clear_static_hosts
fi

HOST_XML="<host mac='${VM_MAC}' ip='${LEASE_IP}'/>"

echo "VM:        $VM_NAME"
echo "Network:   $NET_NAME"
echo "MAC:       $VM_MAC"
echo "Lease IP:  $LEASE_IP"
echo "Pinning lease with: $HOST_XML"

delete_host_entry "$VM_MAC" "$LEASE_IP"

virsh net-update "$NET_NAME" add-last ip-dhcp-host "$HOST_XML" --live
virsh net-update "$NET_NAME" add-last ip-dhcp-host "$HOST_XML" --config

echo "Done. Lease pinned for ${VM_NAME} on '${NET_NAME}' -> ${LEASE_IP}"
