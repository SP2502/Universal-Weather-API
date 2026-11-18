package client

import (
	"errors"
	"fmt"
	"net"
)

var (
	ErrSSRFBlocked = errors.New("outbound request blocked by SSRF security policy")
)

// blockedCIDRs defines network ranges that should never be accessed by outbound requests in production.
var blockedCIDRs = []*net.IPNet{
	// IPv4 Loopback
	mustParseCIDR("127.0.0.0/8"),
	// IPv6 Loopback
	mustParseCIDR("::1/128"),
	// RFC 1918 Private IPv4
	mustParseCIDR("10.0.0.0/8"),
	mustParseCIDR("172.16.0.0/12"),
	mustParseCIDR("192.168.0.0/16"),
	// IPv4 Link-Local & Cloud Metadata (e.g. AWS/GCP/Azure 169.254.169.254)
	mustParseCIDR("169.254.0.0/16"),
	// IPv6 Link-Local
	mustParseCIDR("fe80::/10"),
	// IPv6 Unique Local Address (ULA)
	mustParseCIDR("fc00::/7"),
	// Carrier-Grade NAT
	mustParseCIDR("100.64.0.0/10"),
	// IPv4 "This" Network
	mustParseCIDR("0.0.0.0/8"),
	// Multicast
	mustParseCIDR("224.0.0.0/4"),
	mustParseCIDR("ff00::/8"),
	// Reserved
	mustParseCIDR("240.0.0.0/4"),
}

func mustParseCIDR(cidr string) *net.IPNet {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(fmt.Sprintf("invalid CIDR in SSRF rules: %s (%v)", cidr, err))
	}
	return network
}

// IsBlockedIP returns true if the specified IP address falls into a private, loopback, or cloud-metadata range.
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}

	// Normalize IPv4-mapped IPv6 addresses (e.g. ::ffff:127.0.0.1 -> 127.0.0.1)
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}

	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	for _, network := range blockedCIDRs {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

// ValidateIPs checks all resolved IPs for a host. Returns error if any IP is blocked.
func ValidateIPs(ips []net.IP) error {
	if len(ips) == 0 {
		return fmt.Errorf("%w: no resolved IP addresses", ErrSSRFBlocked)
	}

	for _, ip := range ips {
		if IsBlockedIP(ip) {
			return fmt.Errorf("%w: destination IP %s is prohibited", ErrSSRFBlocked, ip.String())
		}
	}

	return nil
}
