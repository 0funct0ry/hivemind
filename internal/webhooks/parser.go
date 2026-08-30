// Package webhooks normalizes raw incoming-webhook payloads (SPEC.md §4.10) into one internal
// "layout card" shape. Parsers here are pure functions with no store/DB dependency, so they are
// table-tested against fixture payloads independent of the HTTP layer.
package webhooks

import (
	"encoding/json"
)

// Severity buckets a card can resolve to. An unrecognized or absent value is left as "" so the
// caller (internal/store's CreateWebhookMessage) can apply the owning webhook's configured
// default_severity — the parser itself never invents a default.
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
	SeveritySuccess  = "success"
	SeverityNeutral  = "neutral"
)

// ValidSeverities is the whole recognized set, generic.go and slack.go both validate against it.
var ValidSeverities = map[string]bool{
	SeverityCritical: true,
	SeverityWarning:  true,
	SeverityInfo:     true,
	SeveritySuccess:  true,
	SeverityNeutral:  true,
}

// Field is one label/value pair rendered in a card's field grid.
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Card is the normalized shape every format preset parses raw bytes into (SPEC.md §4.10). It is
// intentionally a superset of what any single preset produces — unused fields are left zero.
type Card struct {
	Title          string  `json:"title"`
	Severity       string  `json:"severity"` // "" means unresolved; caller applies the webhook default
	Fields         []Field `json:"fields,omitempty"`
	Body           string  `json:"body"`
	DisplayName    string  `json:"display_name,omitempty"`
	AvatarURL      string  `json:"avatar_url,omitempty"`
	ThreadID       string  `json:"thread_id,omitempty"`
	Fallback       bool    `json:"fallback,omitempty"`
	RedirectNotice bool    `json:"redirect_notice,omitempty"`
}

// PayloadParser normalizes a raw webhook request body into a Card. Parse never returns a hard
// error to the ingest handler in practice — SPEC.md §4.10 requires that a malformed payload
// still post, as a fallback card — but the interface itself does return an error so a parser can
// signal "I could not make sense of this at all" and let the caller build that fallback card;
// see FallbackCard below.
type PayloadParser interface {
	Parse(raw []byte) (Card, error)
}

// maxFallbackBytes is the amount of raw payload SPEC.md §4.10 says to fence into a
// malformed-payload fallback card's body.
const maxFallbackBytes = 2048

// FallbackCard builds the "Unrecognized webhook payload" card SPEC.md §4.10 mandates when a
// parser cannot resolve a title or body/text from the raw bytes.
func FallbackCard(raw []byte) Card {
	body := raw
	if len(body) > maxFallbackBytes {
		body = body[:maxFallbackBytes]
	}
	return Card{
		Title:    "Unrecognized webhook payload",
		Severity: SeverityNeutral,
		Body:     "```\n" + string(body) + "\n```",
		Fallback: true,
	}
}

// ParserFor selects the PayloadParser for a webhook's format_preset. Unknown presets fall back
// to the generic parser rather than erroring — format_preset is validated at webhook
// create/update time, so this is a defensive default, not a user-facing path.
func ParserFor(formatPreset string) PayloadParser {
	if formatPreset == "slack_compatible" {
		return SlackParser{}
	}
	return GenericParser{}
}

// isJSONObject reports whether raw parses as a JSON object at all, used by both parsers to
// decide between "structured but empty" and "not JSON" before falling back.
func isJSONObject(raw []byte) bool {
	var v map[string]any
	return json.Unmarshal(raw, &v) == nil
}
