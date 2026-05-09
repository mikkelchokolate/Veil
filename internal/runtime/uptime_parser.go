package runtime

import (
	"strconv"
	"strings"
)

type UptimeParser struct{}

func NewUptimeParser() UptimeParser { return UptimeParser{} }

func (UptimeParser) Parse(body string) int64 {
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return 0
	}
	secs, _ := strconv.ParseFloat(fields[0], 64)
	return int64(secs)
}
