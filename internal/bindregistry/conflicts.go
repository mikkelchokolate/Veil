package bindregistry

import (
	"fmt"
	"sort"
)

type Conflict struct {
	Key     BindKey
	Owners  []BindOwner
	Message string
}

func ValidateNoConflicts(owners map[BindKey]BindOwner) []Conflict {
	canonical := make(map[BindKey]BindOwner, len(owners))
	for k, o := range owners {
		canonical[k.Canonical()] = o
	}

	keys := make([]BindKey, 0, len(canonical))
	for k := range canonical {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Network != keys[j].Network {
			return keys[i].Network < keys[j].Network
		}
		if keys[i].Address != keys[j].Address {
			return keys[i].Address < keys[j].Address
		}
		return keys[i].Port < keys[j].Port
	})

	var conflicts []Conflict
	for i, k := range keys {
		var overlapping []BindOwner
		for _, otherK := range keys[i+1:] {
			if k.Overlaps(otherK) {
				overlapping = append(overlapping, canonical[otherK])
			}
		}
		if len(overlapping) > 0 {
			conflicts = append(conflicts, Conflict{
				Key:     k,
				Owners:  append([]BindOwner{canonical[k]}, overlapping...),
				Message: fmt.Sprintf("%s %s:%d is claimed by multiple owners", k.Network, k.Address, k.Port),
			})
		}
	}
	return conflicts
}
