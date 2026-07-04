package installer

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

// randomReader is overridable in tests so that RandomHighPort error paths can be
// exercised without mocking the crypto/rand package.
var randomReader = rand.Read

const (
	RandomPortMin = 20000
	RandomPortMax = 50000
)

func RandomHighPort() (int, error) {
	var b [8]byte
	if _, err := randomReader(b[:]); err != nil {
		return 0, err
	}
	n := binary.BigEndian.Uint64(b[:])
	span := uint64(RandomPortMax - RandomPortMin + 1)
	return RandomPortMin + int(n%span), nil
}

func SelectPanelPort(requested int, randomPort func() (int, error)) (port int, random bool, err error) {
	if requested < 0 || requested > 65535 {
		return 0, false, fmt.Errorf("invalid panel port %d", requested)
	}
	if requested > 0 {
		return requested, false, nil
	}
	if randomPort == nil {
		randomPort = RandomHighPort
	}
	port, err = randomPort()
	if err != nil {
		return 0, false, err
	}
	if port <= 0 || port > 65535 {
		return 0, false, fmt.Errorf("random panel port is invalid: %d", port)
	}
	return port, true, nil
}
