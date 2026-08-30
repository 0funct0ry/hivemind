package webhooks

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// SlackParser parses the well-known Slack incoming-webhook payload shape (SPEC.md §4.10):
// top-level text/username/icon_emoji/icon_url, plus attachments[0] with color/title/text/
// fields[]. Only attachments[0] is read; fields[].short is ignored (the card is single-column).
type SlackParser struct{}

type slackPayload struct {
	Text        string            `json:"text"`
	Username    string            `json:"username"`
	IconEmoji   string            `json:"icon_emoji"`
	IconURL     string            `json:"icon_url"`
	Attachments []slackAttachment `json:"attachments"`
}

type slackAttachment struct {
	Color  string       `json:"color"`
	Title  string       `json:"title"`
	Text   string       `json:"text"`
	Fields []slackField `json:"fields"`
}

type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// Parse decodes raw as the Slack-compatible payload shape. Per SPEC.md §4.10, a title/text-less
// payload (or unparseable JSON) resolves to the fallback card rather than a hard error.
func (SlackParser) Parse(raw []byte) (Card, error) {
	var p slackPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return FallbackCard(raw), nil
	}

	var att slackAttachment
	hasAttachment := len(p.Attachments) > 0
	if hasAttachment {
		att = p.Attachments[0]
	}

	title := att.Title
	body := p.Text
	if body == "" {
		body = att.Text
	}
	if title == "" && body == "" {
		return FallbackCard(raw), nil
	}

	var fields []Field
	for _, f := range att.Fields {
		fields = append(fields, Field{Label: f.Title, Value: f.Value})
	}

	avatarURL := p.IconURL

	return Card{
		Title:       title,
		Severity:    slackColorToSeverity(att.Color),
		Fields:      fields,
		Body:        body,
		DisplayName: p.Username,
		AvatarURL:   avatarURL,
	}, nil
}

// slackColorToSeverity maps a Slack attachment's "color" field to one of hivemind's severity
// buckets. The named colors map 1:1 to Slack's own semantics; an arbitrary hex value is mapped
// to whichever of hivemind's severity accent colors it is nearest to in RGB space. An absent or
// unparseable color returns "" (unresolved), matching GenericParser's convention.
func slackColorToSeverity(color string) string {
	switch strings.ToLower(strings.TrimSpace(color)) {
	case "good":
		return SeveritySuccess
	case "warning":
		return SeverityWarning
	case "danger":
		return SeverityCritical
	case "":
		return ""
	}

	r, g, b, ok := parseHexColor(color)
	if !ok {
		return ""
	}

	// The accent colors documented in SPEC.md §4.10 for each severity.
	type bucket struct {
		severity string
		r, g, b  int
	}
	buckets := []bucket{
		{SeverityCritical, 0xB4, 0x40, 0x2A},
		{SeverityWarning, 0xC9, 0x86, 0x0A},
		{SeveritySuccess, 0x0E, 0x6E, 0x60},
		{SeverityNeutral, 0x7C, 0x86, 0x7F},
	}
	best := buckets[0].severity
	bestDist := math.MaxFloat64
	for _, bk := range buckets {
		dr := float64(r - bk.r)
		dg := float64(g - bk.g)
		db := float64(b - bk.b)
		dist := dr*dr + dg*dg + db*db
		if dist < bestDist {
			bestDist = dist
			best = bk.severity
		}
	}
	return best
}

// parseHexColor parses a "#rrggbb" or "rrggbb" string into 0-255 RGB components.
func parseHexColor(s string) (r, g, b int, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	rv, err1 := strconv.ParseInt(s[0:2], 16, 32)
	gv, err2 := strconv.ParseInt(s[2:4], 16, 32)
	bv, err3 := strconv.ParseInt(s[4:6], 16, 32)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return int(rv), int(gv), int(bv), true
}
