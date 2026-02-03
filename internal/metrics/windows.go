package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	WindowsDiscoveryRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "windows_discovery_runs_total",
		Help: "Total number of Windows-native network discovery runs",
	}, []string{"result"}) // "success", "partial", "fail"

	WindowsDiscoveryHostsFound = promauto.NewCounter(prometheus.CounterOpts{
		Name: "windows_discovery_hosts_found_total",
		Help: "Total number of hosts found during Windows-native discovery",
	})

	WindowsFirewallOpsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "windows_firewall_ops_total",
		Help: "Total number of Windows Firewall management operations",
	}, []string{"op", "result"}) // op: install, uninstall, status; result: success, fail
)
