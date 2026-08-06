package interfaceshttpmiddleware

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"

	applicationratelimit "github.com/shiyudesu/frux/internal/application/ratelimit"

	"github.com/cloudwego/hertz/pkg/app"
)

var ErrInvalidTrustedProxy = errors.New("invalid trusted proxy")

type RateLimitIdentityResolver struct {
	trusted []netip.Prefix
}

func NewRateLimitIdentityResolver(trustedProxies []string) (*RateLimitIdentityResolver, error) {
	resolver := &RateLimitIdentityResolver{}
	for _, raw := range trustedProxies {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, ErrInvalidTrustedProxy
		}
		resolver.trusted = append(resolver.trusted, prefix.Masked())
	}
	return resolver, nil
}

func (r *RateLimitIdentityResolver) Resolve(c *app.RequestContext, dimension applicationratelimit.IdentityDimension) (string, bool) {
	if dimension == applicationratelimit.IdentityUser {
		value, exists := c.Get(ContextUserIDKey)
		userID, ok := value.(int64)
		if !exists || !ok || userID <= 0 {
			return "", false
		}
		return "user:" + strconv.FormatInt(userID, 10), true
	}
	ip, ok := r.ClientIP(c.RemoteAddr(), string(c.GetHeader("X-Forwarded-For")), string(c.GetHeader("X-Real-IP")))
	if !ok {
		return "", false
	}
	return "ip:" + ip.String(), true
}

func (r *RateLimitIdentityResolver) ClientIP(remote net.Addr, forwardedFor, realIP string) (netip.Addr, bool) {
	peer, ok := parseAddress(remote)
	if !ok {
		return netip.Addr{}, false
	}
	if !r.trustedAddress(peer) {
		return peer, true
	}
	chain := parseForwardedChain(forwardedFor)
	for index := len(chain) - 1; index >= 0; index-- {
		candidate := chain[index]
		if !r.trustedAddress(candidate) {
			return candidate, true
		}
	}
	if len(chain) > 0 {
		return chain[0], true
	}
	if candidate, err := netip.ParseAddr(strings.TrimSpace(realIP)); err == nil {
		return candidate.Unmap(), true
	}
	return peer, true
}

func (r *RateLimitIdentityResolver) trustedAddress(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range r.trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func parseAddress(address net.Addr) (netip.Addr, bool) {
	if address == nil {
		return netip.Addr{}, false
	}
	raw := strings.TrimSpace(address.String())
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	parsed, err := netip.ParseAddr(strings.Trim(raw, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return parsed.Unmap(), true
}

func parseForwardedChain(raw string) []netip.Addr {
	parts := strings.Split(raw, ",")
	result := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		address, err := netip.ParseAddr(strings.TrimSpace(part))
		if err == nil {
			result = append(result, address.Unmap())
		}
	}
	return result
}
