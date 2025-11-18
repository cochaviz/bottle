package analysis

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"

	"github.com/cochaviz/bottle/internal/sandbox"
)

const (
	natFamily    = "ip"
	natTable     = "lab_nat"
	natChain     = "prerouting"
	filterFamily = "inet"
	filterTable  = "lab_flt"
	filterChain  = "forward"
)

type nftChain struct {
	Family string
	Table  string
	Chain  string
}

var whitelistChains = []nftChain{
	{Family: natFamily, Table: natTable, Chain: natChain},
	{Family: filterFamily, Table: filterTable, Chain: filterChain},
}

var nftCommand = func(args ...string) ([]byte, error) {
	cmd := exec.Command("nft", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("nft %s: %w (output: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func PinDhcpLease(lease sandbox.SandboxLease) error {
	return nil
}

func WhitelistIP(lease sandbox.SandboxLease, ip string) (func() error, error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil, errors.New("c2 ip address is required for whitelisting")
	}
	dest := net.ParseIP(ip)
	if dest == nil {
		return nil, fmt.Errorf("invalid c2 ip address: %s", ip)
	}
	dest = dest.To4()
	if dest == nil {
		return nil, fmt.Errorf("c2 ip must be IPv4: %s", ip)
	}

	vmIP, err := leaseVMIP(lease)
	if err != nil {
		return nil, err
	}

	comment := whitelistComment(vmIP, dest.String())

	exists, err := whitelistRuleExists(comment)
	if err != nil {
		return nil, err
	}

	added := false
	if !exists {
		if err := addWhitelistRules(vmIP, dest.String(), comment); err != nil {
			return nil, err
		}
		slog.Default().Info("whitelisted C2 address", "vm_ip", vmIP, "c2_ip", dest.String())
		added = true
	} else {
		slog.Default().Debug("whitelist rule already exists", "vm_ip", vmIP, "c2_ip", dest.String())
	}

	cleanup := func() error {
		if !added {
			return nil
		}
		if err := removeWhitelistRules(comment); err != nil {
			return err
		}
		slog.Default().Info("removed C2 whitelist", "vm_ip", vmIP, "c2_ip", dest.String())
		return nil
	}

	return cleanup, nil
}

func leaseVMIP(lease sandbox.SandboxLease) (string, error) {
	if lease.Metadata == nil {
		return "", errors.New("sandbox lease metadata missing vm_ip")
	}
	value, ok := lease.Metadata["vm_ip"]
	if !ok {
		return "", errors.New("sandbox lease missing vm_ip metadata")
	}
	text, ok := value.(string)
	if !ok {
		return "", errors.New("sandbox lease vm_ip metadata must be a string")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("sandbox lease vm_ip metadata is empty")
	}
	return text, nil
}

func whitelistComment(vmIP, dstIP string) string {
	return fmt.Sprintf("allow:%s->%s", vmIP, dstIP)
}

func whitelistRuleExists(comment string) (bool, error) {
	needle := []byte(fmt.Sprintf(`comment "%s"`, comment))
	for _, chain := range whitelistChains {
		output, err := nftCommand("list", "chain", chain.Family, chain.Table, chain.Chain)
		if err != nil {
			return false, err
		}
		if !bytes.Contains(output, needle) {
			return false, nil
		}
	}
	return true, nil
}

func addWhitelistRules(vmIP, destIP, comment string) error {
	for _, chain := range whitelistChains {
		args := []string{
			"insert", "rule", chain.Family, chain.Table, chain.Chain,
			"position", "0",
			"ip", "saddr", vmIP,
			"ip", "daddr", destIP,
			"counter", "accept",
			"comment", fmt.Sprintf(`"%s"`, comment),
		}
		if _, err := nftCommand(args...); err != nil {
			return err
		}
	}
	return nil
}

func removeWhitelistRules(comment string) error {
	for _, chain := range whitelistChains {
		handles, err := ruleHandles(chain, comment)
		if err != nil {
			return err
		}
		for _, handle := range handles {
			if _, err := nftCommand("delete", "rule", chain.Family, chain.Table, chain.Chain, "handle", handle); err != nil {
				return err
			}
		}
	}
	return nil
}

func ruleHandles(chain nftChain, comment string) ([]string, error) {
	output, err := nftCommand("-a", "list", "chain", chain.Family, chain.Table, chain.Chain)
	if err != nil {
		return nil, err
	}
	var handles []string
	needle := fmt.Sprintf(`comment "%s"`, comment)
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, needle) {
			continue
		}
		fields := strings.Fields(line)
		for i := 0; i < len(fields); i++ {
			if fields[i] == "handle" && i+1 < len(fields) {
				handle := strings.Trim(fields[i+1], ";")
				handles = append(handles, handle)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return handles, nil
}
