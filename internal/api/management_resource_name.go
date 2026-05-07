package api

import "strings"

type ManagementResourceName struct {
	prefix string
}

func NewManagementResourceName(prefix string) ManagementResourceName {
	return ManagementResourceName{prefix: prefix}
}

func (n ManagementResourceName) Parse(path string) (string, bool) {
	if !strings.HasPrefix(path, n.prefix) {
		return "", false
	}
	name := strings.TrimPrefix(path, n.prefix)
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}
