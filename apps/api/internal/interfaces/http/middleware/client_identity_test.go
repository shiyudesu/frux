package interfaceshttpmiddleware

import (
	"net"
	"net/netip"
	"testing"
)

func TestRateLimitIdentityIgnoresUntrustedForwardingHeaders(t *testing.T) {
	resolver, err := NewRateLimitIdentityResolver([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	address, ok := resolver.ClientIP(
		&net.TCPAddr{IP: net.ParseIP("203.0.113.10"), Port: 443},
		"198.51.100.7", "198.51.100.8",
	)
	if !ok || address != netip.MustParseAddr("203.0.113.10") {
		t.Fatalf("spoofed forwarding header was trusted: %v %v", address, ok)
	}
}

func TestRateLimitIdentityWalksTrustedProxyChain(t *testing.T) {
	resolver, err := NewRateLimitIdentityResolver([]string{"10.0.0.0/8", "192.0.2.0/24"})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	address, ok := resolver.ClientIP(
		&net.TCPAddr{IP: net.ParseIP("10.0.0.5"), Port: 443},
		"198.51.100.9, 192.0.2.20", "",
	)
	if !ok || address != netip.MustParseAddr("198.51.100.9") {
		t.Fatalf("unexpected normalized client: %v %v", address, ok)
	}
}
