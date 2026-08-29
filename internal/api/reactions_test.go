package api

import (
	"encoding/json"
	"testing"
)

func TestReactionAddAPI(t *testing.T) {
	ResetMsgLimiter()
	tc := setupTestContext(t)
	defer tc.close()

	t.Run("add reaction succeeds and returns refreshed array", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "react to me")

		code, resp := tc.request("POST", "/api/v1/messages/"+id+"/reactions", tc.sMember, map[string]any{"emoji": "👍"})
		if code != 200 {
			t.Fatalf("expected 200, got %d. Body: %s", code, resp)
		}
		var out struct {
			Reactions []struct {
				Emoji   string   `json:"emoji"`
				UserIDs []string `json:"user_ids"`
			} `json:"reactions"`
		}
		if err := json.Unmarshal([]byte(resp), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Reactions) != 1 || out.Reactions[0].Emoji != "👍" || len(out.Reactions[0].UserIDs) != 1 {
			t.Fatalf("expected one 👍 reaction with one reactor, got %+v", out.Reactions)
		}
	})

	t.Run("repeat add is idempotent", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "react twice")

		tc.request("POST", "/api/v1/messages/"+id+"/reactions", tc.sMember, map[string]any{"emoji": "🚀"})
		code, resp := tc.request("POST", "/api/v1/messages/"+id+"/reactions", tc.sMember, map[string]any{"emoji": "🚀"})
		if code != 200 {
			t.Fatalf("expected 200, got %d. Body: %s", code, resp)
		}
		var out struct {
			Reactions []struct {
				UserIDs []string `json:"user_ids"`
			} `json:"reactions"`
		}
		_ = json.Unmarshal([]byte(resp), &out)
		if len(out.Reactions) != 1 || len(out.Reactions[0].UserIDs) != 1 {
			t.Fatalf("expected one reactor after repeat add, got %+v", out.Reactions)
		}
	})

	t.Run("empty emoji returns 400 invalid_emoji", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "bad emoji")

		code, resp := tc.request("POST", "/api/v1/messages/"+id+"/reactions", tc.sMember, map[string]any{"emoji": ""})
		if code != 400 {
			t.Fatalf("expected 400, got %d. Body: %s", code, resp)
		}
		assertErrorCode(t, resp, "invalid_emoji")
	})

	t.Run("multi-character emoji returns 400 invalid_emoji", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "bad emoji 2")

		code, resp := tc.request("POST", "/api/v1/messages/"+id+"/reactions", tc.sMember, map[string]any{"emoji": "abc"})
		if code != 400 {
			t.Fatalf("expected 400, got %d. Body: %s", code, resp)
		}
		assertErrorCode(t, resp, "invalid_emoji")
	})

	t.Run("react to missing message returns 404", func(t *testing.T) {
		code, resp := tc.request("POST", "/api/v1/messages/999999/reactions", tc.sMember, map[string]any{"emoji": "👍"})
		if code != 404 {
			t.Fatalf("expected 404, got %d. Body: %s", code, resp)
		}
		assertErrorCode(t, resp, "message_not_found")
	})

	t.Run("react to deleted message returns 404", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "will be deleted")
		tc.request("DELETE", "/api/v1/messages/"+id, tc.sMember, nil)

		code, resp := tc.request("POST", "/api/v1/messages/"+id+"/reactions", tc.sMember, map[string]any{"emoji": "👍"})
		if code != 404 {
			t.Fatalf("expected 404, got %d. Body: %s", code, resp)
		}
		assertErrorCode(t, resp, "message_not_found")
	})

	t.Run("non-member gets 404 on private channel message", func(t *testing.T) {
		id := postMessage(t, tc, tc.privCh.ID, tc.sMember, "private react target")

		code, resp := tc.request("POST", "/api/v1/messages/"+id+"/reactions", tc.sNonMember, map[string]any{"emoji": "👍"})
		if code != 404 {
			t.Fatalf("expected 404, got %d. Body: %s", code, resp)
		}
	})
}

func TestReactionRemoveAPI(t *testing.T) {
	ResetMsgLimiter()
	tc := setupTestContext(t)
	defer tc.close()

	t.Run("author can remove own reaction", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "remove mine")
		tc.request("POST", "/api/v1/messages/"+id+"/reactions", tc.sMember, map[string]any{"emoji": "👍"})

		code, resp := tc.request("DELETE", "/api/v1/messages/"+id+"/reactions/%F0%9F%91%8D", tc.sMember, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d. Body: %s", code, resp)
		}
		var out struct {
			Reactions []any `json:"reactions"`
		}
		_ = json.Unmarshal([]byte(resp), &out)
		if len(out.Reactions) != 0 {
			t.Fatalf("expected no reactions left, got %+v", out.Reactions)
		}
	})

	t.Run("removing a reaction never added is idempotent", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "never reacted")

		code, resp := tc.request("DELETE", "/api/v1/messages/"+id+"/reactions/%F0%9F%91%8D", tc.sMember, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d. Body: %s", code, resp)
		}
	})

	t.Run("removing only removes your own row", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "shared reaction")
		tc.request("POST", "/api/v1/messages/"+id+"/reactions", tc.sMember, map[string]any{"emoji": "🎉"})
		tc.request("POST", "/api/v1/messages/"+id+"/reactions", tc.sAdmin, map[string]any{"emoji": "🎉"})

		code, resp := tc.request("DELETE", "/api/v1/messages/"+id+"/reactions/%F0%9F%8E%89", tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d. Body: %s", code, resp)
		}
		var out struct {
			Reactions []struct {
				UserIDs []string `json:"user_ids"`
			} `json:"reactions"`
		}
		_ = json.Unmarshal([]byte(resp), &out)
		if len(out.Reactions) != 1 || len(out.Reactions[0].UserIDs) != 1 {
			t.Fatalf("expected the other member's reaction to survive, got %+v", out.Reactions)
		}
	})
}

func TestIsSingleEmojiGrapheme(t *testing.T) {
	cases := []struct {
		name  string
		emoji string
		want  bool
	}{
		{"empty string", "", false},
		{"plain ascii single char", "a", true},
		{"plain ascii word", "abc", false},
		{"simple emoji", "🚀", true},
		{"emoji with variation selector", "❤️", true},
		{"emoji with skin tone modifier", "👍🏽", true},
		{"ZWJ family sequence", "👨‍👩‍👧‍👦", true},
		{"flag regional indicator pair", "🇺🇸", true},
		{"two unrelated emoji", "🚀🚀", false},
		{"shortcode text", "rocket", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSingleEmojiGrapheme(tc.emoji); got != tc.want {
				t.Errorf("isSingleEmojiGrapheme(%q) = %v, want %v", tc.emoji, got, tc.want)
			}
		})
	}
}

func assertErrorCode(t *testing.T, resp string, want string) {
	t.Helper()
	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error.Code != want {
		t.Errorf("expected error code %q, got %q", want, out.Error.Code)
	}
}
