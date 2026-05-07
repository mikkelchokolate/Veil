package api

import "fmt"

type RouteDatResponseDecision struct {
	Accept bool
	Retry  bool
	Err    error
}

type RouteDatResponsePolicy struct{}

func NewRouteDatResponsePolicy() RouteDatResponsePolicy { return RouteDatResponsePolicy{} }

func (RouteDatResponsePolicy) Decide(url string, statusCode int, status string) RouteDatResponseDecision {
	if statusCode >= 500 {
		return RouteDatResponseDecision{Retry: true, Err: fmt.Errorf("download %s returned %s", url, status)}
	}
	if statusCode < 200 || statusCode > 299 {
		return RouteDatResponseDecision{Err: fmt.Errorf("download %s returned %s", url, status)}
	}
	return RouteDatResponseDecision{Accept: true}
}
