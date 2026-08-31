package bot

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"user_id":"1"}`)
	secret := "whsec_test"

	if !VerifySignature(secret, body, sign(secret, body)) {
		t.Fatal("expected a correctly signed body to verify")
	}
	if VerifySignature(secret, []byte(`{"user_id":"2"}`), sign(secret, body)) {
		t.Fatal("expected a tampered body to fail verification")
	}
	if VerifySignature("wrong_secret", body, sign(secret, body)) {
		t.Fatal("expected the wrong secret to fail verification")
	}
	if VerifySignature(secret, body, "not-even-hex") {
		t.Fatal("expected a malformed header to fail verification")
	}
	if VerifySignature(secret, body, "") {
		t.Fatal("expected a missing header to fail verification")
	}
}

func TestVerifyTimestamp(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-1 * time.Minute)
	stale := now.Add(-1 * time.Hour)

	toMillis := func(t time.Time) string {
		return strconv.FormatInt(t.UnixMilli(), 10)
	}

	if !VerifyTimestamp(toMillis(fresh), now, 5*time.Minute) {
		t.Fatal("expected a recent timestamp to pass")
	}
	if VerifyTimestamp(toMillis(stale), now, 5*time.Minute) {
		t.Fatal("expected a stale timestamp to fail")
	}
	if !VerifyTimestamp(toMillis(stale), now, 0) {
		t.Fatal("expected maxSkew<=0 to disable the check")
	}
	if VerifyTimestamp("not-a-number", now, 5*time.Minute) {
		t.Fatal("expected a malformed timestamp to fail")
	}
}
