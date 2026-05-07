package api

import (
	"strconv"
	"time"
)

type PingCommandPolicy struct{}

func NewPingCommandPolicy() PingCommandPolicy { return PingCommandPolicy{} }

func (PingCommandPolicy) Timeout(count int) time.Duration {
	return time.Duration(count+2) * time.Second
}

func (PingCommandPolicy) Args(host string, count int) []string {
	return []string{"-c", strconv.Itoa(count), "-W", "2", host}
}
