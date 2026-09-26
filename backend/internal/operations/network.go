package operations

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type Network struct {
	trusted []netip.Prefix
}

func NewNetwork(prefixes []netip.Prefix) Network {
	return Network{trusted: append([]netip.Prefix(nil), prefixes...)}
}

func (n Network) ClientIP(r *http.Request) netip.Addr {
	peer := remoteAddress(r.RemoteAddr)
	if !peer.IsValid() || !n.isTrusted(peer) {
		return peer
	}
	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for index := len(forwarded) - 1; index >= 0; index-- {
		candidate, err := netip.ParseAddr(strings.TrimSpace(forwarded[index]))
		if err != nil {
			continue
		}
		candidate = candidate.Unmap()
		if !n.isTrusted(candidate) {
			return candidate
		}
	}
	return peer
}

func (n Network) IsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	peer := remoteAddress(r.RemoteAddr)
	return peer.IsValid() && n.isTrusted(peer) && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func (n Network) isTrusted(address netip.Addr) bool {
	for _, prefix := range n.trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func remoteAddress(value string) netip.Addr {
	host, _, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		host = strings.TrimSpace(value)
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}
	}
	return address.Unmap()
}
