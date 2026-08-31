package store

import (
	"context"
	"net"
	"testing"
)

// withAllowInsecureWebhookTargets flips the package-level dev/testing escape hatch for the
// duration of a test and restores its original value afterward — this var is normally set once
// at process startup (cmd/serve.go) and must never leak between tests.
func withAllowInsecureWebhookTargets(t *testing.T, v bool) {
	t.Helper()
	orig := AllowInsecureWebhookTargets
	AllowInsecureWebhookTargets = v
	t.Cleanup(func() { AllowInsecureWebhookTargets = orig })
}

func TestValidateOutgoingWebhookTargetURLDefaultRejectsInsecureTargets(t *testing.T) {
	ctx := context.Background()
	withPublicHostResolution(t, "example.com", net.ParseIP("93.184.216.34"))

	if err := validateOutgoingWebhookTargetURL(ctx, "http://example.com/hook"); err == nil {
		t.Fatal("expected http:// to be rejected by default")
	}
	if err := validateOutgoingWebhookTargetURL(ctx, "https://127.0.0.1/hook"); err == nil {
		t.Fatal("expected a loopback target to be rejected by default")
	}
	if err := validateOutgoingWebhookTargetURL(ctx, "https://example.com/hook"); err != nil {
		t.Fatalf("expected a normal public https target to pass, got %v", err)
	}
}

func TestValidateOutgoingWebhookTargetURLAllowInsecureOptIn(t *testing.T) {
	ctx := context.Background()
	withAllowInsecureWebhookTargets(t, true)

	if err := validateOutgoingWebhookTargetURL(ctx, "http://localhost:8091/hooks/echo"); err != nil {
		t.Fatalf("expected http://localhost to pass once opted in, got %v", err)
	}
	if err := validateOutgoingWebhookTargetURL(ctx, "https://127.0.0.1:8091/hooks/echo"); err != nil {
		t.Fatalf("expected a loopback https target to pass once opted in, got %v", err)
	}
	if err := validateOutgoingWebhookTargetURL(ctx, "not a url"); err == nil {
		t.Fatal("expected a malformed URL to still be rejected even when opted in")
	}
	if err := validateOutgoingWebhookTargetURL(ctx, "ftp://example.com"); err == nil {
		t.Fatal("expected a non-http(s) scheme to still be rejected even when opted in")
	}
}
