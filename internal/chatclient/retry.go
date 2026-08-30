package chatclient

import (
	"crypto/rand"
	"encoding/base64"
)

// NewClientMsgID generates a fresh client_msg_id for a composed message. Reusing the same
// value verbatim on an Up-Arrow retry makes POST /messages idempotent (SPEC.md §4.1).
func NewClientMsgID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "cli_" + base64.RawURLEncoding.EncodeToString(b)
}
