package api

import (
	"encoding/json"
	"testing"

	"github.com/0funct0ry/hivemind/internal/store"
)

// TestUpdateMeNamePreservesAvatar guards against a regression where PATCH /users/me
// with only display_name treated the absent avatar_file_id as an explicit null and
// cleared the user's avatar.
func TestUpdateMeNamePreservesAvatar(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	if err := tc.s.InsertFile(tc.ctx, store.File{
		ID: "AVATARFILEID", Sha256: "abc", Name: "avatar.png", Mime: "image/png",
		Size: 10, UploadedBy: tc.uMember.ID, CreatedAt: 1,
	}); err != nil {
		t.Fatalf("insert file: %v", err)
	}

	code, resp := tc.request("PATCH", "/api/v1/users/me", tc.sMember, map[string]any{"avatar_file_id": "AVATARFILEID"})
	if code != 200 {
		t.Fatalf("expected 200 setting avatar, got %d. Resp: %s", code, resp)
	}

	code, resp = tc.request("PATCH", "/api/v1/users/me", tc.sMember, map[string]any{"display_name": "New Name"})
	if code != 200 {
		t.Fatalf("expected 200 updating display name, got %d. Resp: %s", code, resp)
	}

	var out struct {
		User struct {
			DisplayName string `json:"display_name"`
			AvatarURL   string `json:"avatar_url"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		t.Fatalf("unmarshal response: %v. Resp: %s", err, resp)
	}
	if out.User.DisplayName != "New Name" {
		t.Errorf("expected display_name %q, got %q", "New Name", out.User.DisplayName)
	}
	if out.User.AvatarURL == "" {
		t.Errorf("expected avatar_url to survive a name-only update, got empty")
	}
}

// TestUpdateMeExplicitNullClearsAvatar ensures avatar_file_id:null still clears the
// avatar — the tri-state handling must not break the intentional-clear path.
func TestUpdateMeExplicitNullClearsAvatar(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	if err := tc.s.InsertFile(tc.ctx, store.File{
		ID: "AVATARFILEID2", Sha256: "def", Name: "avatar2.png", Mime: "image/png",
		Size: 10, UploadedBy: tc.uMember.ID, CreatedAt: 1,
	}); err != nil {
		t.Fatalf("insert file: %v", err)
	}

	code, resp := tc.request("PATCH", "/api/v1/users/me", tc.sMember, map[string]any{"avatar_file_id": "AVATARFILEID2"})
	if code != 200 {
		t.Fatalf("expected 200 setting avatar, got %d. Resp: %s", code, resp)
	}

	code, resp = tc.request("PATCH", "/api/v1/users/me", tc.sMember, map[string]any{"avatar_file_id": nil})
	if code != 200 {
		t.Fatalf("expected 200 clearing avatar, got %d. Resp: %s", code, resp)
	}

	var out struct {
		User struct {
			AvatarURL string `json:"avatar_url"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		t.Fatalf("unmarshal response: %v. Resp: %s", err, resp)
	}
	if out.User.AvatarURL != "" {
		t.Errorf("expected avatar_url to be cleared, got %q", out.User.AvatarURL)
	}
}
