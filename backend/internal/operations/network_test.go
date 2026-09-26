package operations

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestNetworkIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	network := NewNetwork([]netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")})
	request := httptest.NewRequest("GET", "http://api.example/healthz", nil)
	request.RemoteAddr = "203.0.113.9:4321"
	request.Header.Set("X-Forwarded-For", "198.51.100.7")
	request.Header.Set("X-Forwarded-Proto", "https")
	if got := network.ClientIP(request).String(); got != "203.0.113.9" {
		t.Fatalf("client IP = %s", got)
	}
	if network.IsHTTPS(request) {
		t.Fatal("untrusted peer spoofed HTTPS")
	}
}

func TestNetworkWalksTrustedProxyChainFromRight(t *testing.T) {
	network := NewNetwork([]netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")})
	request := httptest.NewRequest("GET", "http://api.example/healthz", nil)
	request.RemoteAddr = "172.18.0.3:4321"
	request.Header.Set("X-Forwarded-For", "198.51.100.7, 172.18.0.2")
	request.Header.Set("X-Forwarded-Proto", "https")
	if got := network.ClientIP(request).String(); got != "198.51.100.7" {
		t.Fatalf("client IP = %s", got)
	}
	if !network.IsHTTPS(request) {
		t.Fatal("trusted proxy HTTPS was ignored")
	}
}
