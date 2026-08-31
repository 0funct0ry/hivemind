// Package bot implements the `hivemind bot` reference client: a long-running listener that
// answers slash-command webhook calls, plus a one-shot "post" mode for a bot's own proactive
// messages. It deliberately never imports internal/store or any other hivemind-internal
// package — it models what a real third-party integrator writes against the public HTTP
// contract in SPEC.md §4.12, nothing more.
package bot

// CommandExecPayload is the JSON body hivemind POSTs to a slash command's webhook_url
// (SPEC.md §4.12). Field names and shapes are duplicated here on purpose, not imported from
// internal/store/slash_commands.go — see the package doc comment above.
type CommandExecPayload struct {
	UserID    string   `json:"user_id"`
	Username  string   `json:"username"`
	ChannelID string   `json:"channel_id"`
	ThreadID  *string  `json:"thread_id"`
	Args      []string `json:"args"`
}
