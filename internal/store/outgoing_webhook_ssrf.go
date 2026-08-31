package store

import (
	"context"
	"fmt"
	"net"
	"net/url"
)

// resolveHost looks up every IP a host resolves to; it is a var so tests can stub DNS/IP
// resolution without touching the network.
var resolveHost = func(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	var r net.Resolver
	addrs, err := r.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	return addrs, nil
}

// AllowInsecureWebhookTargets disables the SSRF guard's scheme and destination-IP checks below,
// permitting http:// and loopback/private-range/link-local target hosts for outgoing webhooks
// and slash commands. It exists solely so a local, non-production hivemind instance can point a
// webhook_url at a same-machine dev tool (e.g. `hivemind bot`) without a public HTTPS tunnel —
// see scripts/bots/GUIDE.md. It is a package-level var, not a per-request option, because it is
// set exactly once at process startup from `hivemind serve --allow-insecure-webhooks`
// (cmd/serve.go) and never toggled again; production deployments must never enable it, since
// doing so lets anyone who can create or edit a webhook/slash command point hivemind's server at
// its own internal network.
var AllowInsecureWebhookTargets bool

// validateOutgoingWebhookTargetURL enforces SPEC.md §4.11's SSRF guard: target_url must be
// https:// and resolve only to public, non-loopback, non-link-local, non-private-range hosts —
// unless AllowInsecureWebhookTargets has been explicitly opted into for local development.
// Called at create time, at update time when target_url changes, and again at dispatch time
// since DNS can change between the two.
func validateOutgoingWebhookTargetURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("target_url is not a valid URL")
	}
	if u.Scheme != "https" && !(AllowInsecureWebhookTargets && u.Scheme == "http") {
		return fmt.Errorf("target_url must use https://")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("target_url must include a host")
	}

	ips, err := resolveHost(ctx, host)
	if err != nil {
		return fmt.Errorf("target_url host could not be resolved: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("target_url host did not resolve to any address")
	}
	if AllowInsecureWebhookTargets {
		return nil
	}
	for _, ip := range ips {
		if isDisallowedWebhookTargetIP(ip) {
			return fmt.Errorf("target_url must not resolve to a loopback, link-local, or private-range address")
		}
	}
	return nil
}

// isDisallowedWebhookTargetIP reports whether ip is a loopback, link-local, private-range, or
// unspecified address — an outgoing webhook must not become an SSRF vector against the
// server's own network (SPEC.md §4.11).
func isDisallowedWebhookTargetIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

// StubOutgoingWebhookHostResolution overrides DNS resolution for validateOutgoingWebhookTargetURL
// for the duration of a test, so internal/api tests can point target_url at a real host name
// without touching the network or real DNS. Returns a restore func; tests should defer it (or
// pass it to t.Cleanup).
func StubOutgoingWebhookHostResolution(host string, ip net.IP) (restore func()) {
	orig := resolveHost
	resolveHost = func(ctx context.Context, h string) ([]net.IP, error) {
		if h == host {
			return []net.IP{ip}, nil
		}
		return orig(ctx, h)
	}
	return func() { resolveHost = orig }
}
