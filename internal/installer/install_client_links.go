package installer

import (
	"fmt"
	"net/url"
)

type InstallClientLinks struct{}

func NewInstallClientLinks() InstallClientLinks { return InstallClientLinks{} }

func (InstallClientLinks) Naive(username, password, domain string, port int) string {
	u := url.URL{
		Scheme: "https",
		User:   url.UserPassword(username, password),
		Host:   fmt.Sprintf("%s:%d", domain, port),
	}
	return u.String()
}

func (InstallClientLinks) Hysteria2(password, domain string, port int) string {
	u := url.URL{
		Scheme: "hysteria2",
		User:   url.User(password),
		Host:   fmt.Sprintf("%s:%d", domain, port),
	}
	q := u.Query()
	q.Set("insecure", "0")
	u.RawQuery = q.Encode()
	return u.String()
}

func naiveURL(username, password, domain string, port int) string {
	return NewInstallClientLinks().Naive(username, password, domain, port)
}

func hysteria2URI(password, domain string, port int) string {
	return NewInstallClientLinks().Hysteria2(password, domain, port)
}
