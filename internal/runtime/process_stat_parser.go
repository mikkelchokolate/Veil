package runtime

import (
	"strconv"
	"strings"
)

type ProcessStatParser struct{}

type ProcessStatFields struct {
	UserTicks      int64
	SystemTicks    int64
	StartTimeTicks int64
}

func NewProcessStatParser() ProcessStatParser { return ProcessStatParser{} }

func (ProcessStatParser) Parse(body string) (ProcessStatFields, bool) {
	closeParen := strings.LastIndexByte(body, ')')
	if closeParen < 0 {
		return ProcessStatFields{}, false
	}
	fields := strings.Fields(body[closeParen+2:])
	if len(fields) < 20 {
		return ProcessStatFields{}, false
	}
	utime, _ := strconv.ParseInt(fields[11], 10, 64)
	stime, _ := strconv.ParseInt(fields[12], 10, 64)
	starttime, _ := strconv.ParseInt(fields[19], 10, 64)
	return ProcessStatFields{UserTicks: utime, SystemTicks: stime, StartTimeTicks: starttime}, true
}
