package api

import "github.com/veil-panel/veil/internal/managementstate"

type ManagementResourceName = managementstate.ResourceNameParser

func NewManagementResourceName(prefix string) ManagementResourceName {
	return managementstate.NewResourceNameParser(prefix)
}
