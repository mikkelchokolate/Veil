package api

import "fmt"

type RouteDatBodyLimit struct {
	max int
}

func NewRouteDatBodyLimit(max int) RouteDatBodyLimit {
	if max <= 0 {
		max = maxRouteDatSize
	}
	return RouteDatBodyLimit{max: max}
}

func (l RouteDatBodyLimit) Limit() int { return l.max }

func (l RouteDatBodyLimit) Validate(url string, body []byte) error {
	if len(body) > l.max {
		return fmt.Errorf("download %s exceeds maximum size of %d bytes", url, l.max)
	}
	return nil
}
