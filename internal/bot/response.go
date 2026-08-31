package bot

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CommandResponse is the JSON shape hivemind expects back from a slash command's webhook_url
// (SPEC.md §4.12): {"response_type": "ephemeral"|"in_channel", "text"?, "attachments"?}.
type CommandResponse struct {
	ResponseType string          `json:"response_type"`
	Text         string          `json:"text,omitempty"`
	Attachments  json.RawMessage `json:"attachments,omitempty"`
}

const (
	ResponseTypeEphemeral = "ephemeral"
	ResponseTypeInChannel = "in_channel"
)

// ValidResponseType reports whether v is one of the two response_type values hivemind accepts.
func ValidResponseType(v string) bool {
	return v == ResponseTypeEphemeral || v == ResponseTypeInChannel
}

// BuildResponse maps one script execution's outcome to the response hivemind will receive.
//
// A non-zero exit code, or runErr (the script couldn't even start, or hit its timeout), always
// produces an ephemeral card carrying the failure detail — regardless of what the script wrote
// to stdout — so a tester always sees why their script failed, distinct from the "the webhook
// didn't respond at all" case that a real network failure or a bad signature already tests via
// a non-2xx HTTP status.
//
// On a clean exit, stdout that parses as JSON with a recognized response_type is passed through
// verbatim (attachments included) so a script has full control for testing every response
// shape; anything else (plain text, or JSON missing/misusing response_type) is wrapped as
// {response_type: defaultType, text: <trimmed stdout>}.
func BuildResponse(stdout, stderr []byte, exitCode int, runErr error, defaultType string) CommandResponse {
	if runErr != nil {
		return CommandResponse{ResponseType: ResponseTypeEphemeral, Text: runErr.Error()}
	}
	if exitCode != 0 {
		text := fmt.Sprintf("script exited %d", exitCode)
		if s := strings.TrimSpace(string(stderr)); s != "" {
			text += ": " + s
		}
		return CommandResponse{ResponseType: ResponseTypeEphemeral, Text: text}
	}

	trimmed := strings.TrimSpace(string(stdout))
	var parsed CommandResponse
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil && ValidResponseType(parsed.ResponseType) {
		return parsed
	}

	if !ValidResponseType(defaultType) {
		defaultType = ResponseTypeEphemeral
	}
	return CommandResponse{ResponseType: defaultType, Text: trimmed}
}
