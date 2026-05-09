package runtime

import (
	"strconv"
	"strings"
)

type LoadAvgParser struct{}

func NewLoadAvgParser() LoadAvgParser { return LoadAvgParser{} }

func (LoadAvgParser) Parse(body string) loadAvg {
	fields := strings.Fields(body)
	if len(fields) < 3 {
		return loadAvg{}
	}
	avg1, _ := strconv.ParseFloat(fields[0], 64)
	avg5, _ := strconv.ParseFloat(fields[1], 64)
	avg15, _ := strconv.ParseFloat(fields[2], 64)
	return loadAvg{avg1: avg1, avg5: avg5, avg15: avg15}
}
