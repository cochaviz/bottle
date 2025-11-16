#!/usr/bin/env bash
# whitelist_ip_for_vm.sh — add/remove a single bypass of INetSim for one VM → one real IP
# Usage:
#   Add:    sudo ./whitelist_ip_for_vm.sh 10.13.37.42 93.184.216.34 [WAN_IF]
#   Remove: sudo ./whitelist_ip_for_vm.sh -r 10.13.37.42 93.184.216.34
#   List:   sudo ./whitelist_ip_for_vm.sh -l
set -euo pipefail

# --- config: adjust only if you renamed tables/chains ---
NAT_FAM=ip
NAT_TABLE=lab_nat
NAT_CHAIN=prerouting

FLT_FAM=inet
FLT_TABLE=lab_flt
FLT_CHAIN=forward

usage() {
  sed -n '2,12p' "$0" >&2
  exit 1
}

need() { command -v "$1" >/dev/null || { echo "Missing: $1" >&2; exit 1; }; }

list_rules() {
  nft list chain "$NAT_FAM" "$NAT_TABLE" "$NAT_CHAIN" 2>/dev/null | sed -n '1,200p' | grep -n 'comment "allow:' || true
  nft list chain "$FLT_FAM" "$FLT_TABLE" "$FLT_CHAIN" 2>/dev/null | sed -n '1,200p' | grep -n 'comment "allow:' || true
}

ensure_exists() {
  nft list chain "$NAT_FAM" "$NAT_TABLE" "$NAT_CHAIN" >/dev/null
  nft list chain "$FLT_FAM" "$FLT_TABLE" "$FLT_CHAIN" >/dev/null
}

rule_exists() {
  local comment=$1
  nft list chain "$NAT_FAM" "$NAT_TABLE" "$NAT_CHAIN" 2>/dev/null | grep -q -- "comment \"$comment\"" || return 1
  nft list chain "$FLT_FAM" "$FLT_TABLE" "$FLT_CHAIN" 2>/dev/null | grep -q -- "comment \"$comment\"" || return 1
}

add_rule() {
  local vm_ip=$1 dst_ip=$2 wan_if=${3:-}
  local comment="allow:${vm_ip}->${dst_ip}"

  if rule_exists "$comment"; then
    echo "[=] Already present: $comment"; return 0
  fi

  nft insert rule "$NAT_FAM" "$NAT_TABLE" "$NAT_CHAIN" position 0 \
    ip saddr "$vm_ip" ip daddr "$dst_ip" counter accept \
    comment "\"allow:${vm_ip}->${dst_ip}\""

  if [[ -n "${wan_if}" ]]; then
    nft insert rule "$FLT_FAM" "$FLT_TABLE" "$FLT_CHAIN" position 0 \
      ip saddr "$vm_ip" ip daddr "$dst_ip" oifname "$wan_if" counter accept \
      comment "\"allow:${vm_ip}->${dst_ip}\""
  else
    nft insert rule "$FLT_FAM" "$FLT_TABLE" "$FLT_CHAIN" position 0 \
      ip saddr "$vm_ip" ip daddr "$dst_ip" counter accept \
      comment "\"allow:${vm_ip}->${dst_ip}\""
  fi

  echo "[+] Added: $comment"
}

del_rule() {
  local vm_ip=$1 dst_ip=$2
  local comment="allow:${vm_ip}->${dst_ip}"

  # Delete by handle for both chains (grep comment → extract handle)
  for fam in "$NAT_FAM" "$FLT_FAM"; do
    local table chain
    if [[ $fam == "$NAT_FAM" ]]; then table=$NAT_TABLE; chain=$NAT_CHAIN; else table=$FLT_TABLE; chain=$FLT_CHAIN; fi

    # list with handles (-a), filter comment, delete each handle
    while read -r handle; do
      [[ -z "${handle:-}" ]] && continue
      nft delete rule "$fam" "$table" "$chain" handle "$handle" || true
    done < <(nft -a list chain "$fam" "$table" "$chain" 2>/dev/null \
              | awk -v cmt="$comment" '
                  index($0, "comment \"" cmt "\"") {
                    for (i=1; i<=NF; i++) if ($i=="handle") { print $(i+1); break }
                  }')
  done

  echo "[-] Removed: $comment (if it existed)"
}

main() {
  [[ $EUID -eq 0 ]] || { echo "Run as root." >&2; exit 1; }
  need nft

  local mode="add"
  if [[ "${1:-}" == "-r" || "${1:-}" == "--remove" ]]; then mode="del"; shift; fi
  if [[ "${1:-}" == "-l" || "${1:-}" == "--list" ]];   then list_rules; exit 0; fi

  [[ $# -ge 2 ]] || usage
  local vm_ip=$1 dst_ip=$2 wan_if=${3:-}

  ensure_exists

  case "$mode" in
    add) add_rule "$vm_ip" "$dst_ip" "$wan_if" ;;
    del) del_rule "$vm_ip" "$dst_ip" ;;
  esac
}

main "$@"
