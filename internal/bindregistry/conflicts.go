package bindregistry

import "fmt"

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
	var conflicts []Conflict
	for k, owner := range canonical {
		var overlapping []BindOwner
		for otherK, otherOwner := range canonical {
			if otherK == k {
				continue
			}
			if k.Overlaps(otherK) {
				overlapping = append(overlapping, otherOwner)
			}
		}
		if len(overlapping) > 0 {
			conflicts = append(conflicts, Conflict{
				Key:     k,
				Owners:  append([]BindOwner{owner}, overlapping...),
				Message: fmt.Sprintf("%s %s:%d is claimed by multiple owners", k.Network, k.Address, k.Port),
			})
		}
	}
	return conflicts
}
