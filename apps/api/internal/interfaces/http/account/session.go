package interfaceshttpaccount

import (
	"mime"
	"net/url"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

func refreshCredentialFromCookie(c *app.RequestContext) string {
	return strings.TrimSpace(string(c.Cookie("frux_refresh_token")))
}

func validJSONRequest(c *app.RequestContext) bool {
	mediaType, _, err := mime.ParseMediaType(
		strings.TrimSpace(string(c.GetHeader("Content-Type"))),
	)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func validSessionRequestOrigin(c *app.RequestContext) bool {
	rawOrigin := strings.TrimSpace(string(c.GetHeader("Origin")))
	if rawOrigin == "" {
		return true
	}
	origin, err := url.Parse(rawOrigin)
	if err != nil || origin.Host == "" {
		return false
	}
	hosts := []string{
		strings.TrimSpace(string(c.Host())),
		firstForwardedValue(string(c.GetHeader("X-Forwarded-Host"))),
	}
	matchedHost := false
	for _, host := range hosts {
		if host != "" && strings.EqualFold(origin.Host, host) {
			matchedHost = true
			break
		}
	}
	if !matchedHost {
		return false
	}
	expectedScheme := "http"
	if strings.EqualFold(strings.TrimSpace(string(c.GetHeader("X-Forwarded-Proto"))), "https") {
		expectedScheme = "https"
	}
	return strings.EqualFold(origin.Scheme, expectedScheme)
}

func firstForwardedValue(raw string) string {
	value, _, _ := strings.Cut(raw, ",")
	return strings.TrimSpace(value)
}
