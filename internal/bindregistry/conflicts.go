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
	// Iterate raw keys in a fixed order so the surviving owner of a canonical
	// collapse — and every reported owner list — is deterministic.
	rawKeys := make([]BindKey, 0, len(owners))
	for k := range owners {
		rawKeys = append(rawKeys, k)
	}
	sort.Slice(rawKeys, func(i, j int) bool {
		if rawKeys[i].Network != rawKeys[j].Network {
			return rawKeys[i].Network < rawKeys[j].Network
		}
		if rawKeys[i].Address != rawKeys[j].Address {
			return rawKeys[i].Address < rawKeys[j].Address
		}
		return rawKeys[i].Port < rawKeys[j].Port
	})

	canonical := make(map[BindKey]BindOwner, len(owners))
	// Distinct raw keys can canonicalize to the same bind ("" and "0.0.0.0"
	// both mean the wildcard). A last-writer-wins collapse would silently
	// lose an owner and let a real double-bind pass validation (#1225), so
	// colliding claims are collected and reported like any other conflict.
	collisions := make(map[BindKey][]BindOwner)
	for _, k := range rawKeys {
		ck := k.Canonical()
		owner := owners[k]
		if existing, taken := canonical[ck]; taken {
			if existing != owner {
				if collisions[ck] == nil {
					collisions[ck] = []BindOwner{existing}
				}
				dup := false
				for _, prev := range collisions[ck] {
					if prev == owner {
						dup = true
						break
					}
				}
				if !dup {
					collisions[ck] = append(collisions[ck], owner)
				}
			}
			continue
		}
		canonical[ck] = owner
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
	for _, k := range keys {
		if colliding, ok := collisions[k]; ok && len(colliding) > 1 {
			conflicts = append(conflicts, Conflict{
				Key:     k,
				Owners:  colliding,
				Message: fmt.Sprintf("%s %s:%d is claimed by multiple owners", k.Network, k.Address, k.Port),
			})
		}
	}
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
