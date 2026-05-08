package installer

import (
	"crypto/rand"
	"encoding/binary"
)

const (
	RandomPortMin = 20000
	RandomPortMax = 50000
)

func RandomHighPort() (int, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	n := binary.BigEndian.Uint64(b[:])
	span := uint64(RandomPortMax - RandomPortMin + 1)
	return RandomPortMin + int(n%span), nil
}
