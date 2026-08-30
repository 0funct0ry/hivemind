package webhooks

import "testing"

func TestSlackParser_Parse(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantTitle    string
		wantBody     string
		wantSeverity string
		wantFallback bool
		wantFields   int
	}{
		{
			name:         "text plus attachment",
			raw:          `{"text":"fallback text","username":"CI Bot","icon_url":"https://x","attachments":[{"color":"good","title":"Build passed","text":"all green","fields":[{"title":"Branch","value":"main","short":true}]}]}`,
			wantTitle:    "Build passed",
			wantBody:     "fallback text",
			wantSeverity: "success",
			wantFields:   1,
		},
		{
			name:         "danger color maps to critical",
			raw:          `{"attachments":[{"color":"danger","title":"Build failed"}]}`,
			wantTitle:    "Build failed",
			wantSeverity: "critical",
		},
		{
			name:         "warning color maps to warning",
			raw:          `{"attachments":[{"color":"warning","title":"Flaky test"}]}`,
			wantTitle:    "Flaky test",
			wantSeverity: "warning",
		},
		{
			name:         "hex color maps to nearest bucket",
			raw:          `{"attachments":[{"color":"#B4402A","title":"Custom red"}]}`,
			wantTitle:    "Custom red",
			wantSeverity: "critical",
		},
		{
			name:         "text only, no attachments",
			raw:          `{"text":"hello world"}`,
			wantBody:     "hello world",
			wantSeverity: "",
		},
		{
			name:         "empty payload falls back",
			raw:          `{}`,
			wantFallback: true,
		},
		{
			name:         "malformed json falls back",
			raw:          `<xml>not json</xml>`,
			wantFallback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card, err := SlackParser{}.Parse([]byte(tt.raw))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if card.Fallback != tt.wantFallback {
				t.Fatalf("Fallback = %v, want %v", card.Fallback, tt.wantFallback)
			}
			if tt.wantFallback {
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
			if len(card.Fields) != tt.wantFields {
				t.Fatalf("len(Fields) = %d, want %d", len(card.Fields), tt.wantFields)
			}
		})
	}
}
