package runtime

import (
	"strconv"
	"strings"
)

type ProcessMemoryParser struct{}

func NewProcessMemoryParser() ProcessMemoryParser { return ProcessMemoryParser{} }

func (ProcessMemoryParser) Parse(body string) int64 {
	fields := strings.Fields(body)
	if len(fields) < 2 {
		return 0
	}
	rssPages, _ := strconv.ParseInt(fields[1], 10, 64)
	return rssPages * 4 / 1024
}
