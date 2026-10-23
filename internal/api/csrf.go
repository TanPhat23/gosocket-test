package api

import (
	"net/url"
	"slices"
	"strings"

	"agent-task-manager/internal/auth"

	"github.com/gofiber/fiber/v3"
)

const headerSecFetchSite = "Sec-Fetch-Site"

// CSRFProtection rejects unsafe-method requests (anything but GET/HEAD/OPTIONS/TRACE) that carry
// the atm_session cookie unless they come from a trusted origin (see trustedOrigin). Any request
// with the cookie is checked, even if it also sends a Bearer PAT, because RequireAuth prefers the
// cookie. Requests authenticated only by a Bearer PAT are not checked: browsers never attach
// Authorization headers cross-site on their own.
func CSRFProtection(allowedOrigins []string) fiber.Handler {
	return originGuard(allowedOrigins, func(c fiber.Ctx) bool {
		return c.Cookies(auth.SessionCookieName) != ""
	})
}

// RequireTrustedOrigin applies the same origin check to every unsafe-method request regardless of
// credentials. It protects login and logout, which a cross-site page could otherwise drive to swap
// or drop the victim's session (login CSRF).
func RequireTrustedOrigin(allowedOrigins []string) fiber.Handler {
	return originGuard(allowedOrigins, func(fiber.Ctx) bool { return true })
}

func originGuard(allowedOrigins []string, applies func(fiber.Ctx) bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		if isSafeMethod(c.Method()) || !applies(c) || trustedOrigin(c, allowedOrigins) {
			return c.Next()
		}
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Forbidden: cross-site request rejected",
		})
	}
}

func isSafeMethod(method string) bool {
	switch method {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions, fiber.MethodTrace:
		return true
	}
	return false
}

// trustedOrigin decides whether a request was issued by the API's own origin or a configured one:
//   - Sec-Fetch-Site "same-origin" or "none" (user-initiated) is trusted;
//   - any other Sec-Fetch-Site value ("same-site", "cross-site") is trusted only when Origin is one
//     of the configured allowed origins (a separately hosted frontend is cross-origin by definition);
//   - without Sec-Fetch-Site, Origin (falling back to Referer) must name the request host or a
//     configured allowed origin.
//
// A request with no usable signal is rejected (fail closed).
func trustedOrigin(c fiber.Ctx, allowedOrigins []string) bool {
	switch c.Get(headerSecFetchSite) {
	case "same-origin", "none":
		return true
	case "":
	default:
		return configuredOrigin(c.Get(fiber.HeaderOrigin), allowedOrigins)
	}

	source := c.Get(fiber.HeaderOrigin)
	if source == "" {
		source = c.Get(fiber.HeaderReferer)
	}
	return originAllowed(source, string(c.RequestCtx().Host()), allowedOrigins)
}

// originAllowed reports whether a browser Origin (or a Referer URL) names the same host[:port] the
// request was sent to, or equals one of the configured allowed origins.
func originAllowed(raw, requestHost string, allowedOrigins []string) bool {
	u, ok := parseBrowserOrigin(raw)
	if !ok {
		return false
	}
	return strings.EqualFold(u.Host, requestHost) || slices.Contains(allowedOrigins, originString(u))
}

// configuredOrigin reports whether raw equals one of the configured allowed origins.
func configuredOrigin(raw string, allowedOrigins []string) bool {
	u, ok := parseBrowserOrigin(raw)
	return ok && slices.Contains(allowedOrigins, originString(u))
}

func parseBrowserOrigin(raw string) (*url.URL, bool) {
	if raw == "" {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, false
	}
	return u, true
}

// originString renders u's origin in the lowercase form config.LoadSecurityConfig produces.
func originString(u *url.URL) string {
	return strings.ToLower(u.Scheme + "://" + u.Host)
}
