package windows

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type DiscoveredHost struct {
	IP        string `json:"ip"`
	Interface string `json:"interface"`
	Source    string `json:"source"` // "wmi", "arp", "probe"
}

type DiscoveryConfig struct {
	MaxHosts   int
	TimeBudget time.Duration
	Probe      bool
}

type wmiResult struct {
	NICs []struct {
		InterfaceAlias string `json:"InterfaceAlias"`
		IPAddress      string `json:"IPAddress"`
		PrefixLength   int    `json:"PrefixLength"`
	} `json:"nics"`
	Neighbors []struct {
		IPAddress string `json:"IPAddress"`
		State     string `json:"State"`
	} `json:"neighbors"`
}

// ScanLAN performs a Windows-native discovery of hosts on the local network.
// It queries network adapters and the ARP table (neighbors).
// If cfg.Probe is true, it may perform active probing (bounded).
func ScanLAN(ctx context.Context, cfg DiscoveryConfig) ([]DiscoveredHost, error) {
	if cfg.TimeBudget == 0 {
		cfg.TimeBudget = 30 * time.Second
	}
	if cfg.MaxHosts == 0 {
		cfg.MaxHosts = 1024
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.TimeBudget)
	defer cancel()

	// PowerShell script to gather NICs and Neighbors
	script := `
$ErrorActionPreference = 'SilentlyContinue'
$nics = Get-NetIPAddress -AddressFamily IPv4 -Type Unicast | Select-Object InterfaceAlias, IPAddress, PrefixLength
$neighbors = Get-NetNeighbor -AddressFamily IPv4 | Select-Object IPAddress, State
$out = @{ nics = $nics; neighbors = $neighbors }
$out | ConvertTo-Json -Compress
`

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", script)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("discovery: timed out after %v", cfg.TimeBudget)
		}
		// Check for permission issues
		return nil, fmt.Errorf("discovery: powershell execution failed: %w", err)
	}

	var res wmiResult
	if err := json.Unmarshal(output, &res); err != nil {
		// Handle partial results if JSON is malformed or empty
		return nil, fmt.Errorf("discovery: failed to parse WMI output: %w", err)
	}

	seen := make(map[string]bool)
	var hosts []DiscoveredHost

	// 1. Process NICs (our own addresses)
	for _, nic := range res.NICs {
		if len(hosts) >= cfg.MaxHosts {
			break
		}
		if !seen[nic.IPAddress] {
			hosts = append(hosts, DiscoveredHost{
				IP:        nic.IPAddress,
				Interface: nic.InterfaceAlias,
				Source:    "wmi",
			})
			seen[nic.IPAddress] = true
		}
	}

	// 2. Process Neighbors (ARP table)
	for _, nb := range res.Neighbors {
		if len(hosts) >= cfg.MaxHosts {
			break
		}
		if nb.IPAddress != "" && !seen[nb.IPAddress] {
			hosts = append(hosts, DiscoveredHost{
				IP:     nb.IPAddress,
				Source: "arp",
			})
			seen[nb.IPAddress] = true
		}
	}

	return hosts, nil
}
