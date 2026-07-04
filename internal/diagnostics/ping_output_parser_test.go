package diagnostics

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

func TestPingOutputParserParsesBsdStyleOutput(t *testing.T) {
	output := `PING example.com (93.184.216.34): 56 data bytes
64 bytes from 93.184.216.34: icmp_seq=0 ttl=56 time=12.345 ms

3 packets transmitted, 2 packets received, 33.3% packet loss
round-trip min/avg/max/stddev = 10.100/12.200/15.300/1.400 ms
`
	result := PingResult{Host: "example.com"}
	NewPingOutputParser().Parse(output, &result)
	if result.Transmitted != 3 || result.Received != 2 || result.MinMs != 10.1 || result.AvgMs != 12.2 || result.MaxMs != 15.3 || result.StddevMs != 1.4 {
		t.Fatalf("result = %+v", result)
	}
}

func TestPingOutputParserHandlesEmptyOutput(t *testing.T) {
	result := PingResult{Host: "example.com", Transmitted: 3}
	NewPingOutputParser().Parse("", &result)
	if result.Transmitted != 3 || result.Received != 0 || result.MinMs != 0 || result.AvgMs != 0 || result.MaxMs != 0 || result.StddevMs != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestPingOutputParserIgnoresMalformedLines(t *testing.T) {
	output := `PING example.com (93.184.216.34): 56 data bytes
this is not a stats line
rtt min/avg/max/mdev = 10/20/30/4 ms
missing stats
5 packets transmitted, 4 received
rtt line without equals
latency = 1.0/2.0 ms
`
	result := PingResult{Host: "example.com"}
	NewPingOutputParser().Parse(output, &result)
	if result.Transmitted != 5 || result.Received != 4 || result.MinMs != 10 || result.AvgMs != 20 || result.MaxMs != 30 || result.StddevMs != 4 {
		t.Fatalf("result = %+v", result)
	}
}

func TestPingOutputParserHandlesPartialLatencyStats(t *testing.T) {
	output := `5 packets transmitted, 5 received
rtt min/avg/max/mdev = 10.000/12.000/14.000/1.000 ms
`
	result := PingResult{Host: "example.com"}
	NewPingOutputParser().Parse(output, &result)
	if result.Transmitted != 5 || result.Received != 5 || result.MinMs != 10 || result.AvgMs != 12 || result.MaxMs != 14 || result.StddevMs != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestParsePingOutputWrapper(t *testing.T) {
	output := `2 packets transmitted, 1 received
rtt min/avg/max/mdev = 1.000/2.000/3.000/0.500 ms
`
	result := PingResult{Host: "example.com"}
	ParsePingOutput(output, &result)
	if result.Transmitted != 2 || result.Received != 1 || result.MinMs != 1 || result.AvgMs != 2 || result.MaxMs != 3 || result.StddevMs != 0.5 {
		t.Fatalf("result = %+v", result)
	}
}

func TestPrivateParsePingOutputWrapper(t *testing.T) {
	output := `4 packets transmitted, 3 received
rtt min/avg/max/mdev = 5.000/6.000/7.000/0.250 ms
`
	result := PingResult{Host: "example.com"}
	parsePingOutput(output, &result)
	if result.Transmitted != 4 || result.Received != 3 || result.MinMs != 5 || result.AvgMs != 6 || result.MaxMs != 7 || result.StddevMs != 0.25 {
		t.Fatalf("result = %+v", result)
	}
}
