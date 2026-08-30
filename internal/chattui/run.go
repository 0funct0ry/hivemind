package chattui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/0funct0ry/hivemind/internal/chatclient"
	"github.com/ergochat/readline"
)

const maxScrollbackLines = 200

// Run drives the chat TUI's main loop: it wires a restartable realtime event pump (its own
// goroutine, rebuilt whenever .login/.logout change the client's token) together with the
// synchronous readline input loop, redrawing the frame on both new input and pushed events. It
// returns when the user issues .quit or input reaches EOF.
func Run(ctx context.Context, client *chatclient.Client, me chatclient.User, host, wsBaseURL string, insecure bool, historyPath string) error {
	app := NewApp(client, me)
	app.Host = host
	renderer := NewRenderer()

	rl, err := NewInput(historyPath, app)
	if err != nil {
		return err
	}
	defer rl.Close()

	var mu sync.Mutex
	redraw := func(status string) {
		mu.Lock()
		defer mu.Unlock()
		frame := renderer.Frame(app.HeaderLine(), app.Lines, "", maxScrollbackLines)
		_ = Flush(frame)
		if status != "" {
			fmt.Fprintln(rl.Stderr(), status)
		}
		rl.Refresh()
	}

	// PromptCredentials holds the same mutex redraw() does for its entire duration, so a WS
	// event arriving mid-prompt can't trigger a concurrent redraw() that repaints the screen
	// underneath it. It also writes the "Password: " prompt itself before calling
	// ReadPassword: this readline version's ReadPassword(prompt) silently ignores the prompt
	// argument (GenPasswordConfig never copies Prompt from the base config, so the generated
	// masked-input config's prompt is always ""), which is what made the password prompt
	// render blank. ReadPassword's own line-reading loop queries the cursor position before
	// drawing anything and starts from there rather than clearing the line, so text written
	// just beforehand survives.
	app.PromptCredentials = func() (string, string, error) {
		mu.Lock()
		defer mu.Unlock()
		rl.SetPrompt("Username: ")
		defer rl.SetPrompt("> ")
		username, err := rl.Readline()
		if err != nil {
			return "", "", err
		}
		fmt.Fprint(rl.Stdout(), "Password: ")
		password, err := rl.ReadPassword("")
		if err != nil {
			return "", "", err
		}
		return strings.TrimSpace(username), string(password), nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	conn := startConnManager(runCtx, client, wsBaseURL, insecure)
	app.Reconnect = conn.restart
	go func() {
		for ev := range conn.events {
			handleEvent(app, ev)
			redraw("")
		}
	}()

	for {
		line, err := rl.ReadLine()
		if err != nil {
			if errors.Is(err, readline.ErrInterrupt) || errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		status, dispatchErr := app.Dispatch(runCtx, line)
		if dispatchErr != nil {
			if errors.Is(dispatchErr, ErrQuit) {
				return nil
			}
			status = dispatchErr.Error()
		}
		if cid := app.CurrentChannelID(); cid != "" && len(app.Lines) > 0 {
			last := app.Lines[len(app.Lines)-1]
			conn.setCursor(cid, strconv.FormatInt(last.TS, 10))
		}
		redraw(status)
	}
}

// connManager owns the WebSocket connection's lifecycle across .login/.logout, exposing a
// merged event stream, a restart trigger, and a way to record resume cursors against whichever
// ManagedConn is currently active.
type connManager struct {
	events  <-chan chatclient.Event
	restart chan struct{}

	mu      sync.Mutex
	current *chatclient.ManagedConn
}

func (c *connManager) setCursor(channelID, messageID string) {
	c.mu.Lock()
	mc := c.current
	c.mu.Unlock()
	if mc != nil {
		mc.SetCursor(channelID, messageID)
	}
}

// startConnManager dials once immediately and re-dials whenever a value arrives on the
// returned restart channel (sent by App.Login/Logout after they change the client's token),
// always using client.Token() at the moment of (re)connect. The merged event stream never
// closes on a restart — only when ctx is done.
func startConnManager(ctx context.Context, client *chatclient.Client, wsBaseURL string, insecure bool) *connManager {
	merged := make(chan chatclient.Event, 64)
	cm := &connManager{events: merged, restart: make(chan struct{}, 1)}

	go func() {
		defer close(merged)
		for {
			connCtx, cancel := context.WithCancel(ctx)
			mc := chatclient.NewManagedConn(chatclient.WSURLFromBase(wsBaseURL), client.Token(), insecure)
			cm.mu.Lock()
			cm.current = mc
			cm.mu.Unlock()
			go mc.Run(connCtx)

			pumpDone := make(chan struct{})
			go func() {
				defer close(pumpDone)
				for ev := range mc.Events() {
					select {
					case merged <- ev:
					case <-connCtx.Done():
						return
					}
				}
			}()

			select {
			case <-cm.restart:
				cancel()
				<-pumpDone
			case <-ctx.Done():
				cancel()
				<-pumpDone
				return
			}
		}
	}()

	return cm
}

// handleEvent applies one realtime event to app's state. Only events relevant to the
// currently-open channel/thread are rendered; everything else is a no-op here (the CLI keeps
// no cross-channel unread state, unlike the web client).
func handleEvent(app *App, ev chatclient.Event) {
	switch ev.Type {
	case "message.created", "thread.reply":
		var payload struct {
			Message   *chatclient.Message `json:"message"`
			ChannelID string              `json:"channel_id"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			return
		}
		msg := payload.Message
		if msg == nil {
			var direct chatclient.Message
			if err := json.Unmarshal(ev.Payload, &direct); err != nil {
				return
			}
			msg = &direct
		}
		if msg.ChannelID != app.CurrentChannelID() {
			return
		}
		app.appendMessage(context.Background(), *msg)
	case "_reconnected":
		// A resume completed; the caller already backfilled cursors are tracked via
		// setCursor as messages render, so nothing further is needed here beyond the
		// redraw the caller already triggers.
	}
}
