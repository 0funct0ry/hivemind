package webhooks

import "encoding/json"

// GenericParser parses hivemind's own JSON webhook shape (SPEC.md §4.10):
//
//	{
//	  "title": "...", "severity": "critical", "fields": [{"label":"...","value":"..."}],
//	  "body": "...", "display_name": "...", "avatar_url": "...", "thread_id": "1892"
//	}
type GenericParser struct{}

type genericPayload struct {
	Title       string  `json:"title"`
	Severity    string  `json:"severity"`
	Fields      []Field `json:"fields"`
	Body        string  `json:"body"`
	DisplayName string  `json:"display_name"`
	AvatarURL   string  `json:"avatar_url"`
	ThreadID    string  `json:"thread_id"`
}

// Parse decodes raw as the generic payload shape. Per SPEC.md §4.10, at least one of
// title/body is required; a payload satisfying neither (or unparseable JSON) resolves to the
// fallback card rather than a hard error.
func (GenericParser) Parse(raw []byte) (Card, error) {
	var p genericPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return FallbackCard(raw), nil
	}
	if p.Title == "" && p.Body == "" {
		return FallbackCard(raw), nil
	}

	severity := p.Severity
	if !ValidSeverities[severity] {
		severity = "" // unresolved; caller applies the webhook's default_severity
	}

	return Card{
		Title:       p.Title,
		Severity:    severity,
		Fields:      p.Fields,
		Body:        p.Body,
		DisplayName: p.DisplayName,
		AvatarURL:   p.AvatarURL,
		ThreadID:    p.ThreadID,
	}, nil
}
