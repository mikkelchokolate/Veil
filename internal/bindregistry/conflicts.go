package bindregistry

import (
	"fmt"
	"sort"
	"strings"
)

// Conflict describes two owners whose listener keys overlap.
type Conflict struct {
	Key     BindKey
	Owners  []BindOwner
	Message string
}

type ownedBind struct {
	key   BindKey
	owner BindOwner
}

// ValidateNoConflicts reports every pair of overlapping listener owners. The
// result order is stable so API errors and tests do not depend on map ordering.
func ValidateNoConflicts(owners map[BindKey]BindOwner) []Conflict {
	entries := make([]ownedBind, 0, len(owners))
	for key, owner := range owners {
		entries = append(entries, ownedBind{key: key.Canonical(), owner: owner})
	}
	sort.Slice(entries, func(i, j int) bool {
		left := entries[i].key.String() + "\x00" + ownerDescription(entries[i].owner)
		right := entries[j].key.String() + "\x00" + ownerDescription(entries[j].owner)
		return left < right
	})

	conflicts := make([]Conflict, 0)
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if !entries[i].key.Overlaps(entries[j].key) {
				continue
			}
			first := entries[i]
			second := entries[j]
			conflicts = append(conflicts, Conflict{
				Key:    first.key,
				Owners: []BindOwner{first.owner, second.owner},
				Message: fmt.Sprintf(
					"%s owned by %s conflicts with %s owned by %s",
					first.key.String(), ownerDescription(first.owner),
					second.key.String(), ownerDescription(second.owner),
				),
			})
		}
	}
	return conflicts
}

func ownerDescription(owner BindOwner) string {
	parts := []string{string(owner.Kind)}
	if owner.InboundName != "" {
		parts = append(parts, fmt.Sprintf("inbound %q", owner.InboundName))
	}
	if owner.ServiceName != "" {
		parts = append(parts, fmt.Sprintf("service %q", owner.ServiceName))
	}
	return strings.Join(parts, " ")
}
