package api

import "strings"

type MeminfoParser struct{}

func NewMeminfoParser() MeminfoParser { return MeminfoParser{} }

func (MeminfoParser) Parse(body string) memInfo {
	var total, avail int64
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = parseKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			avail = parseKB(line)
		}
	}
	if total == 0 {
		return memInfo{}
	}
	return memInfo{total: total, used: total - avail}
}
