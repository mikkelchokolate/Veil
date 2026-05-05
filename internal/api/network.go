package api

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// NetworkInterface stats from /proc/net/dev.
type NetworkInterface struct {
	Name      string `json:"name"`
	RxBytes   int64  `json:"rxBytes"`
	TxBytes   int64  `json:"txBytes"`
	RxPackets int64  `json:"rxPackets"`
	TxPackets int64  `json:"txPackets"`
}

// NetworkStats holds per-interface network statistics.
type NetworkStats struct {
	Interfaces []NetworkInterface `json:"interfaces"`
}

// readNetworkStats parses /proc/net/dev for interface counters.
func readNetworkStats() (NetworkStats, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return NetworkStats{}, err
	}
	defer f.Close()

	var stats NetworkStats
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Inter-") || strings.HasPrefix(line, "face") {
			continue
		}
		// Format: name: rxBytes rxPackets ... txBytes txPackets ...
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		fields := strings.Fields(parts[1])
		if len(fields) < 10 {
			continue
		}
		rxBytes, _ := strconv.ParseInt(fields[0], 10, 64)
		rxPackets, _ := strconv.ParseInt(fields[1], 10, 64)
		txBytes, _ := strconv.ParseInt(fields[8], 10, 64)
		txPackets, _ := strconv.ParseInt(fields[9], 10, 64)

		stats.Interfaces = append(stats.Interfaces, NetworkInterface{
			Name:      name,
			RxBytes:   rxBytes,
			TxBytes:   txBytes,
			RxPackets: rxPackets,
			TxPackets: txPackets,
		})
	}
	return stats, nil
}
