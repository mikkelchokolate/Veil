package managementstate

import "strings"

type ResourceNameParser struct {
	prefix string
}

func NewResourceNameParser(prefix string) ResourceNameParser {
	return ResourceNameParser{prefix: prefix}
}

func (p ResourceNameParser) Parse(path string) (string, bool) {
	if !strings.HasPrefix(path, p.prefix) {
		return "", false
	}
	name := strings.TrimPrefix(path, p.prefix)
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}
