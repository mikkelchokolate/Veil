package runtimeports

import (
	"fmt"
	"net"
	"strconv"
)

// Hysteria2TrafficStatsPort is reserved for the local-only Hysteria2 Traffic
// Stats API. Each enabled Hysteria2 inbound binds this port on a distinct
// 127/8 address derived from its unique public UDP port, so multiple Hysteria2
// processes can expose accounting without colliding with each other.
const Hysteria2TrafficStatsPort = 61000

// Hysteria2TrafficStatsHost maps a validated public UDP port to a stable,
// distinct loopback address. Linux treats the entire 127/8 prefix as loopback.
// Public Hysteria2 ports are unique in desired-state validation, making this
// mapping collision-free across enabled Hysteria2 inbounds.
func Hysteria2TrafficStatsHost(publicPort int) string {
	if publicPort < 1 || publicPort > 65535 {
		return "127.0.0.1"
	}
	return fmt.Sprintf("127.%d.%d.1", (publicPort>>8)&0xff, publicPort&0xff)
}

func Hysteria2TrafficStatsAddress(publicPort int) string {
	return net.JoinHostPort(Hysteria2TrafficStatsHost(publicPort), strconv.Itoa(Hysteria2TrafficStatsPort))
}

func Hysteria2TrafficStatsEndpoint(publicPort int) string {
	return "http://" + Hysteria2TrafficStatsAddress(publicPort) + "/traffic"
}
