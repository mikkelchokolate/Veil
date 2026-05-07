package api

import "testing"

func TestPingOutputParserParsesPacketAndLatencyStats(t *testing.T) {
	output := `PING example.com (93.184.216.34): 56 data bytes
64 bytes from 93.184.216.34: icmp_seq=0 ttl=56 time=12.345 ms

3 packets transmitted, 2 received, 33% packet loss
rtt min/avg/max/mdev = 10.100/12.200/15.300/1.400 ms
`
	result := PingResult{Host: "example.com"}
	NewPingOutputParser().Parse(output, &result)
	if result.Transmitted != 3 || result.Received != 2 || result.MinMs != 10.1 || result.AvgMs != 12.2 || result.MaxMs != 15.3 || result.StddevMs != 1.4 {
		t.Fatalf("result = %+v", result)
	}
}
