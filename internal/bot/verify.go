package bot

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// VerifySignature reports whether header (the request's X-Hivemind-Signature value) is a valid
// "sha256=<hex hmac>" signature of body under secret — the same format
// internal/store/outgoing_webhooks.go's signOutgoingWebhookBody produces for both outgoing
// webhooks and slash commands. Uses hmac.Equal for a constant-time comparison.
func VerifySignature(secret string, body []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// VerifyTimestamp reports whether header (the request's X-Hivemind-Timestamp value, unix
// millis) falls within maxSkew of now — a basic replay-window check. maxSkew <= 0 disables the
// check (always returns true), so this stays optional for testers who don't need it.
func VerifyTimestamp(header string, now time.Time, maxSkew time.Duration) bool {
	if maxSkew <= 0 {
		return true
	}
	ms, err := strconv.ParseInt(header, 10, 64)
	if err != nil {
		return false
	}
	sentAt := time.UnixMilli(ms)
	delta := now.Sub(sentAt)
	if delta < 0 {
		delta = -delta
	}
	return delta <= maxSkew
}
