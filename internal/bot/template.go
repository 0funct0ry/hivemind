package bot

import (
	"fmt"
	"strings"
	"text/template"
)

// TemplateData is the data made available to a script's Go template placeholders — both in the
// slash-command listener (populated from the incoming CommandExecPayload) and in `hivemind bot
// post` (populated from CLI flags).
type TemplateData struct {
	// Trigger is the slash command's trigger, e.g. "/status" (empty for `bot post`).
	Trigger string
	// Args are the slash command's positional arguments, or extra args passed to `bot post`.
	Args []string
	// ArgsJoined is Args joined with a single space, for the common case of just forwarding them.
	ArgsJoined string
	// UserID and Username identify the invoking member (empty for `bot post`, which has no
	// invoking user — it posts as the bot itself).
	UserID   string
	Username string
	// ChannelID is the channel the command was run in, or the --channel target for `bot post`.
	ChannelID string
	// ThreadID is the thread being replied into, if any.
	ThreadID string
	// Vars holds arbitrary key=value pairs from `bot post --var`, empty in the listener.
	Vars map[string]string
	// Time is the render time in RFC3339, and Hostname is the local machine's hostname — both
	// useful in scripts that want to stamp their output.
	Time     string
	Hostname string
}

var templateFuncs = template.FuncMap{
	"join": strings.Join,
}

// Render parses content as a Go text/template and executes it against data. Errors carry
// enough context (parse vs. execute) to be actionable when a tester's script has a typo.
func Render(content string, data TemplateData) (string, error) {
	tmpl, err := template.New("script").Funcs(templateFuncs).Parse(content)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var out strings.Builder
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return out.String(), nil
}
