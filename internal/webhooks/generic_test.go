package webhooks

import "testing"

func TestGenericParser_Parse(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantTitle    string
		wantBody     string
		wantSeverity string
		wantFallback bool
	}{
		{
			name:         "full payload",
			raw:          `{"title":"SEV-2","severity":"critical","fields":[{"label":"Rate","value":"14.2%"}],"body":"Timeouts climbing","display_name":"Datadog","avatar_url":"https://x","thread_id":"1892"}`,
			wantTitle:    "SEV-2",
			wantBody:     "Timeouts climbing",
			wantSeverity: "critical",
		},
		{
			name:         "unknown severity leaves it unresolved",
			raw:          `{"title":"Build OK","severity":"bogus"}`,
			wantTitle:    "Build OK",
			wantSeverity: "",
		},
		{
			name:         "absent severity leaves it unresolved",
			raw:          `{"title":"Build OK"}`,
			wantTitle:    "Build OK",
			wantSeverity: "",
		},
		{
			name:         "body only is accepted",
			raw:          `{"body":"just a body"}`,
			wantBody:     "just a body",
			wantSeverity: "",
		},
		{
			name:         "missing title and body falls back",
			raw:          `{"fields":[{"label":"x","value":"y"}]}`,
			wantFallback: true,
		},
		{
			name:         "malformed json falls back",
			raw:          `not json at all`,
			wantFallback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card, err := GenericParser{}.Parse([]byte(tt.raw))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if card.Fallback != tt.wantFallback {
				t.Fatalf("Fallback = %v, want %v", card.Fallback, tt.wantFallback)
			}
			if tt.wantFallback {
				if card.Title != "Unrecognized webhook payload" {
					t.Fatalf("fallback title = %q", card.Title)
				}
				return
			}
			if card.Title != tt.wantTitle {
				t.Fatalf("Title = %q, want %q", card.Title, tt.wantTitle)
			}
			if card.Body != tt.wantBody {
				t.Fatalf("Body = %q, want %q", card.Body, tt.wantBody)
			}
			if card.Severity != tt.wantSeverity {
				t.Fatalf("Severity = %q, want %q", card.Severity, tt.wantSeverity)
			}
		})
	}
}

func TestFallbackCard_TruncatesTo2KB(t *testing.T) {
	raw := make([]byte, 5000)
	for i := range raw {
		raw[i] = 'x'
	}
	card := FallbackCard(raw)
	if !card.Fallback {
		t.Fatal("expected Fallback=true")
	}
	if len(card.Body) > maxFallbackBytes+len("```\n\n```") {
		t.Fatalf("fallback body too long: %d bytes", len(card.Body))
	}
}
