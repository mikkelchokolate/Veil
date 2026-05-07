package api

import (
	"strconv"
	"strings"
)

type CPUTicksParser struct{}

func NewCPUTicksParser() CPUTicksParser { return CPUTicksParser{} }

func (CPUTicksParser) Parse(body string) (idle, total uint64) {
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return 0, 0
		}
		for i := 1; i < len(fields); i++ {
			v, _ := strconv.ParseUint(fields[i], 10, 64)
			total += v
			if i == 4 {
				idle = v
			}
		}
		return idle, total
	}
	return 0, 0
}
