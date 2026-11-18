package client

import (
	"net"
	"net/url"
	"strings"
)

// IsLoopbackTestEndpoint is only for adapter constructors used by local tests.
// Production registration supplies fixed vendor endpoints, never request URLs.
func IsLoopbackTestEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
