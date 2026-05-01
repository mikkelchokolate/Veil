package installer

import "fmt"

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
