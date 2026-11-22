package framework

import (
	"fmt"
	"net"
	"strings"
)

// parseTrustedProxies turns a list of IPs and CIDR ranges into networks.
func parseTrustedProxies(items []string) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if ip := net.ParseIP(item); ip != nil {
			bits := 8 * net.IPv6len
			if v4 := ip.To4(); v4 != nil {
				ip, bits = v4, 8*net.IPv4len
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, network, err := net.ParseCIDR(item)
		if err != nil {
			return nil, fmt.Errorf("framework: invalid trusted proxy %q: not an IP address or CIDR range", item)
		}
		nets = append(nets, network)
	}
	return nets, nil
}

func isTrusted(ip net.IP, trusted []*net.IPNet) bool {
	for _, network := range trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// remoteHost returns the host part of an "ip:port" address.
func remoteHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// resolveClientIP finds the real client behind trusted proxies.
//
// Forwarding headers are only believed when the directly connected peer is a
// trusted proxy. X-Forwarded-For is then read from the right: every hop that is
// itself a trusted proxy is skipped and the first other address is the client.
// A malformed entry makes the header untrustworthy, so the peer address is used.
func resolveClientIP(remote string, forwardedFor []string, realIP string, trusted []*net.IPNet) string {
	peer := net.ParseIP(remote)
	if peer == nil || !isTrusted(peer, trusted) {
		return remote
	}

	var hops []string
	for _, header := range forwardedFor {
		for _, hop := range strings.Split(header, ",") {
			if hop = strings.TrimSpace(hop); hop != "" {
				hops = append(hops, hop)
			}
		}
	}

	for i := len(hops) - 1; i >= 0; i-- {
		ip := net.ParseIP(hops[i])
		if ip == nil {
			return remote
		}
		if !isTrusted(ip, trusted) || i == 0 {
			return ip.String()
		}
	}

	if ip := net.ParseIP(strings.TrimSpace(realIP)); ip != nil {
		return ip.String()
	}
	return remote
}

// ClientIP returns the IP address of the client. Without WithTrustedProxies it
// is the directly connected peer. With it, X-Forwarded-For and X-Real-IP are
// honored, but only when the peer is one of the trusted proxies.
func (c *Context) ClientIP() string {
	remote := remoteHost(c.Request.RemoteAddr)
	if len(c.server.trusted) == 0 {
		return remote
	}
	return resolveClientIP(remote, c.Request.Header.Values("X-Forwarded-For"), c.Request.Header.Get("X-Real-Ip"), c.server.trusted)
}

// Scheme returns "https" or "http". The connection's own scheme is used unless
// the peer is a trusted proxy that sent X-Forwarded-Proto.
func (c *Context) Scheme() string {
	scheme := c.Request.Scheme
	if scheme == "" {
		scheme = "http"
	}
	if len(c.server.trusted) == 0 {
		return scheme
	}
	peer := net.ParseIP(remoteHost(c.Request.RemoteAddr))
	if peer == nil || !isTrusted(peer, c.server.trusted) {
		return scheme
	}
	switch forwarded := strings.ToLower(c.Request.Header.Get("X-Forwarded-Proto")); forwarded {
	case "http", "https":
		return forwarded
	}
	return scheme
}
