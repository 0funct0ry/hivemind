package chattui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/0funct0ry/hivemind/internal/chatclient"
)

// ErrQuit signals that the user issued .quit and the run loop should exit cleanly.
var ErrQuit = errors.New("quit")

// Command describes one dot-command for dispatch, .help output, and tab completion — the
// single source of truth so all three can never drift out of sync with each other.
type Command struct {
	Name  string // including the leading '.'
	Usage string
	Help  string
}

// Commands is every dot-command hivemind chat recognizes, in the order .help lists them.
var Commands = []Command{
	{Name: ".join", Usage: ".join <channel>", Help: "Switch the current view to <channel> (name or #slug), resetting message tags."},
	{Name: ".dm", Usage: ".dm <username>", Help: "Open (or start) a direct message conversation with <username>."},
	{Name: ".thread", Usage: ".thread <tag>", Help: "Open the thread rooted at the message tagged <tag>."},
	{Name: ".leave", Usage: ".leave", Help: "Leave the current channel/DM view. Rejoin any time with .join or .dm — does not affect server-side membership."},
	{Name: ".clear", Usage: ".clear", Help: "Clear the message pane and reset message tags. Does not affect server state."},
	{Name: ".who", Usage: ".who", Help: "List the members of the current channel."},
	{Name: ".channels", Usage: ".channels", Help: "List every public channel."},
	{Name: ".users", Usage: ".users", Help: "List every user in the workspace."},
	{Name: ".login", Usage: ".login", Help: "Log in again (prompts for username and password)."},
	{Name: ".logout", Usage: ".logout", Help: "Log out and clear the cached session."},
	{Name: ".help", Usage: ".help", Help: "Show this list of commands."},
	{Name: ".quit", Usage: ".quit", Help: "Close the connection and exit."},
}

// viewState is either a channel or an open thread within one. displayName is precomputed at
// .join/.dm time rather than derived from channel.Name on every render, since a DM channel
// carries no name of its own — only a peer/members list.
type viewState struct {
	channel     chatclient.Channel
	displayName string
	threadID    *string // non-nil when a .thread view is open
}

// App holds everything the dot-command dispatcher and the realtime event loop need to share:
// the network client, the current view, tag assignment, per-user colors, and retry state for
// Up-Arrow resends. PromptCredentials and Reconnect are wired up by chattui.Run — App itself
// stays free of direct terminal I/O and connection-lifecycle ownership.
type App struct {
	Client     *chatclient.Client
	Tags       *chatclient.TagMap
	Colors     *ColorCache
	Me         chatclient.User
	Lines      []Line
	Host       string
	view       *viewState
	lastSentID map[string]string // literal composed line -> client_msg_id, for Up-Arrow reuse
	seenIDs    map[int64]bool    // message ids already rendered in the current view

	// PromptCredentials, when set, reads a username and password from the terminal for
	// .login. It is nil in tests exercising Dispatch without a terminal.
	PromptCredentials func() (username, password string, err error)
	// Reconnect, when set, is signaled (non-blocking) after .login/.logout change the
	// client's token, so the run loop can restart the WebSocket connection.
	Reconnect chan struct{}
}

// NewApp constructs an App for the given authenticated user.
func NewApp(client *chatclient.Client, me chatclient.User) *App {
	return &App{
		Client:     client,
		Tags:       chatclient.NewTagMap(),
		Colors:     NewColorCache(),
		Me:         me,
		lastSentID: map[string]string{},
		seenIDs:    map[int64]bool{},
	}
}

// CurrentChannelID returns the id of the channel currently in view, or "" if none.
func (a *App) CurrentChannelID() string {
	if a.view == nil {
		return ""
	}
	return a.view.channel.ID
}

// HeaderLine renders the current view's name, or "" when no channel/DM is open — an empty
// header is simply omitted by Renderer.Frame rather than showing a permanent placeholder bar.
func (a *App) HeaderLine() string {
	if a.view == nil {
		return ""
	}
	label := a.view.displayName
	if a.view.threadID != nil {
		label += " · thread"
	}
	return label
}

func (a *App) resetView() {
	a.Tags.Reset()
	a.seenIDs = map[int64]bool{}
	a.Lines = nil
}

// appendMessage assigns a tag and appends a rendered Line for msg. A message already seen in
// this view — the sender's own local echo followed by the WS message.created fanout for the
// same id — is rendered once, not twice: seenIDs is the dedup signal, checked before Assign
// (which would otherwise silently return the existing tag and let a second Line through).
func (a *App) appendMessage(ctx context.Context, msg chatclient.Message) {
	id, err := parseID(msg.ID)
	if err != nil {
		return
	}
	if a.seenIDs[id] {
		return
	}
	a.seenIDs[id] = true
	tag := a.Tags.Assign(id)
	name := msg.UserID
	color := 0
	if msg.User != nil {
		name = msg.User.DisplayName
		if name == "" {
			name = msg.User.Username
		}
		color = a.Colors.Get(msg.User.ID, msg.User.AvatarColor)
	}
	a.Lines = append(a.Lines, Line{
		Tag:         tag,
		TS:          msg.CreatedAt,
		AuthorName:  name,
		AuthorColor: color,
		Body:        msg.Body,
		Reverse:     HighlightsYou(msg.Body),
	})
}

// findChannel resolves a channel by name or slug (with or without a leading '#') against the
// cached channel list, refreshing it from the server first.
func (a *App) findChannel(ctx context.Context, name string) (chatclient.Channel, bool, error) {
	name = strings.TrimPrefix(name, "#")
	channels, err := a.Client.ListChannels(ctx)
	if err != nil {
		return chatclient.Channel{}, false, err
	}
	for _, ch := range channels {
		if ch.Slug != nil && *ch.Slug == name {
			return ch, true, nil
		}
		if ch.Name == name {
			return ch, true, nil
		}
	}
	return chatclient.Channel{}, false, nil
}

// openView loads a channel's history into a fresh view, per SPEC.md §7.7 — shared by Join and
// DM so both reset tags/lines/seenIDs the same way.
func (a *App) openView(ctx context.Context, ch chatclient.Channel, displayName string) error {
	page, err := a.Client.GetMessages(ctx, ch.ID, nil, nil, 50)
	if err != nil {
		return err
	}
	a.view = &viewState{channel: ch, displayName: displayName}
	a.resetView()
	for i := len(page.Data) - 1; i >= 0; i-- {
		a.appendMessage(ctx, page.Data[i])
	}
	return nil
}

// Join switches the current view to the named channel, resetting tags, per SPEC.md §7.7. The
// exact copy on failure matches the 404-not-403 posture: a nonexistent or inaccessible channel
// gets the same message.
func (a *App) Join(ctx context.Context, name string) (string, error) {
	trimmed := strings.TrimPrefix(name, "#")
	notFound := fmt.Sprintf("#%s does not exist or you lack sufficient access permissions.", trimmed)

	ch, ok, err := a.findChannel(ctx, name)
	if err != nil {
		return "", err
	}
	if !ok {
		return notFound, nil
	}
	if err := a.openView(ctx, ch, "#"+trimmed); err != nil {
		var apiErr *chatclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			return notFound, nil
		}
		return "", err
	}
	return "", nil
}

// DM opens (or starts) a direct message conversation with the named user.
func (a *App) DM(ctx context.Context, username string) (string, error) {
	username = strings.TrimPrefix(username, "@")
	if username == "" {
		return "usage: .dm <username>", nil
	}
	users, err := a.Client.ListUsers(ctx, username, 10)
	if err != nil {
		return "", err
	}
	var target *chatclient.User
	for i, u := range users {
		if strings.EqualFold(u.Username, username) {
			target = &users[i]
			break
		}
	}
	if target == nil {
		return fmt.Sprintf("no user named %q was found.", username), nil
	}
	dm, err := a.Client.CreateDM(ctx, []string{target.ID})
	if err != nil {
		return "", err
	}
	ch := chatclient.Channel{ID: dm.ID, Kind: dm.Kind, MemberCount: dm.MemberCount}
	name := target.DisplayName
	if name == "" {
		name = target.Username
	}
	if err := a.openView(ctx, ch, "@"+name); err != nil {
		return "", err
	}
	return "", nil
}

// Thread opens the thread rooted at the tagged message, resetting tags to the thread's own
// scope.
func (a *App) Thread(ctx context.Context, tag string) (string, error) {
	id, ok := a.Tags.Resolve(tag)
	if !ok {
		return fmt.Sprintf("no message tagged %q in the current view.", tag), nil
	}
	if a.view == nil {
		return "join a channel first.", nil
	}
	msgID := formatID(id)
	a.view.threadID = &msgID
	a.resetView()
	return "", nil
}

// Clear clears the message pane and resets tags without affecting server state.
func (a *App) Clear() {
	a.resetView()
}

// Leave closes the current channel/DM view client-side only — it never calls the server's
// membership-removing /channels/:id/leave, so re-opening the same conversation later with
// .join or .dm picks up exactly where it left off, full history included. This mirrors DMs'
// own "hide" semantics (POST /dms/:id/hide, SPEC.md §4.2) generalized to channels too.
func (a *App) Leave() (string, error) {
	if a.view == nil {
		return "no conversation is open.", nil
	}
	name := a.view.displayName
	a.view = nil
	a.resetView()
	return fmt.Sprintf("left %s. Rejoin any time with .join or .dm.", name), nil
}

// Who lists the current channel's members.
func (a *App) Who(ctx context.Context) (string, error) {
	if a.view == nil {
		return "join a channel first.", nil
	}
	members, err := a.Client.Members(ctx, a.view.channel.ID)
	if err != nil {
		return "", err
	}
	return joinDisplayNames(members), nil
}

// Channels lists every public channel in the workspace, column-formatted with each name in a
// stable, distinct color (HashColor, since a channel has no avatar_color of its own).
func (a *App) Channels(ctx context.Context) (string, error) {
	channels, err := a.Client.ListChannels(ctx)
	if err != nil {
		return "", err
	}
	var items, plain []string
	for _, ch := range channels {
		if ch.Kind != "public" {
			continue
		}
		text := "#" + ch.Name
		items = append(items, FgSGR(HashColor(ch.ID))+text+ansiReset)
		plain = append(plain, text)
	}
	if len(items) == 0 {
		return "no public channels.", nil
	}
	return FormatColumns(items, plain), nil
}

// Users lists every user in the workspace, column-formatted with each entry in that user's own
// avatar color and showing both display name and username, per SPEC.md §7.7's per-user color
// convention.
func (a *App) Users(ctx context.Context) (string, error) {
	users, err := a.Client.ListUsers(ctx, "", 200)
	if err != nil {
		return "", err
	}
	var items, plain []string
	for _, u := range users {
		text := u.Username
		if u.DisplayName != "" && u.DisplayName != u.Username {
			text = fmt.Sprintf("%s (%s)", u.DisplayName, u.Username)
		}
		items = append(items, FgSGR(a.Colors.Get(u.ID, u.AvatarColor))+text+ansiReset)
		plain = append(plain, text)
	}
	if len(items) == 0 {
		return "no users.", nil
	}
	return FormatColumns(items, plain), nil
}

func joinDisplayNames(users []chatclient.User) string {
	names := make([]string, 0, len(users))
	for _, m := range users {
		name := m.DisplayName
		if name == "" {
			name = m.Username
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// signalReconnect asks the run loop to restart the WebSocket connection with the client's
// current token, non-blocking so Login/Logout never stall waiting on a full channel.
func (a *App) signalReconnect() {
	if a.Reconnect == nil {
		return
	}
	select {
	case a.Reconnect <- struct{}{}:
	default:
	}
}

// Login prompts for credentials (via PromptCredentials) and re-authenticates, mirroring
// cmd/chat.go's initial login flow: a fresh bearer token is minted and cached, replacing
// whatever token (if any) the client currently holds.
func (a *App) Login(ctx context.Context) (string, error) {
	if a.PromptCredentials == nil {
		return "login is unavailable in this context.", nil
	}
	username, password, err := a.PromptCredentials()
	if err != nil {
		return "", err
	}
	cookie, err := a.Client.Login(ctx, username, password)
	if err != nil {
		return err.Error(), nil
	}
	token, err := a.Client.IssueToken(ctx, cookie, "chat-cli", 720*time.Hour)
	if err != nil {
		return err.Error(), nil
	}
	a.Client.SetToken(token)
	if err := chatclient.SaveSession(&chatclient.Session{
		Host:      a.Host,
		Token:     token,
		ExpiresAt: time.Now().Add(720 * time.Hour).UnixMilli(),
	}); err != nil {
		return "", err
	}
	me, err := a.Client.Me(ctx)
	if err != nil {
		return "", err
	}
	a.Me = me
	a.view = nil
	a.resetView()
	a.signalReconnect()
	return fmt.Sprintf("logged in as %s.", a.Me.Username), nil
}

// Logout clears the client's bearer token and the cached session file, dropping the current
// view — every subsequent call needs .login again (or a relaunch with --token/credentials).
func (a *App) Logout(ctx context.Context) (string, error) {
	a.Client.SetToken("")
	if path, err := chatclient.SessionPath(); err == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	a.view = nil
	a.resetView()
	a.signalReconnect()
	return "logged out.", nil
}

// Help renders the usage and help text for every dot-command, for the .help command.
func (a *App) Help() string {
	var b strings.Builder
	b.WriteString("Commands:\n")
	for _, c := range Commands {
		fmt.Fprintf(&b, "  %-20s %s\n", c.Usage, c.Help)
	}
	b.WriteString("Anything not starting with '.' is sent as a message to the current channel.")
	return b.String()
}

// Send posts a bare (non-dot) line as a message, reusing the same client_msg_id verbatim if
// this exact line text was already sent and failed (Up-Arrow retry, SPEC.md §7.7).
func (a *App) Send(ctx context.Context, body string) error {
	if a.view == nil {
		return errors.New("join a channel first with .join <channel>")
	}
	clientMsgID, ok := a.lastSentID[body]
	if !ok {
		clientMsgID = chatclient.NewClientMsgID()
	}
	msg, err := a.Client.PostMessage(ctx, a.view.channel.ID, body, clientMsgID, a.view.threadID)
	if err != nil {
		a.lastSentID[body] = clientMsgID
		return err
	}
	delete(a.lastSentID, body)
	a.appendMessage(ctx, msg)
	return nil
}

// Dispatch handles one input line: a dot-command, or a bare send. It returns a status message
// to display (may be empty) and ErrQuit when the user issued .quit.
func (a *App) Dispatch(ctx context.Context, line string) (string, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", nil
	}
	if !strings.HasPrefix(line, ".") {
		return "", a.Send(ctx, line)
	}
	fields := strings.SplitN(line, " ", 2)
	cmd := fields[0]
	var arg string
	if len(fields) > 1 {
		arg = strings.TrimSpace(fields[1])
	}
	switch cmd {
	case ".join":
		return a.Join(ctx, arg)
	case ".dm":
		return a.DM(ctx, arg)
	case ".thread":
		return a.Thread(ctx, arg)
	case ".leave":
		return a.Leave()
	case ".clear":
		a.Clear()
		return "", nil
	case ".who":
		return a.Who(ctx)
	case ".channels":
		return a.Channels(ctx)
	case ".users":
		return a.Users(ctx)
	case ".login":
		return a.Login(ctx)
	case ".logout":
		return a.Logout(ctx)
	case ".help":
		return a.Help(), nil
	case ".quit":
		return "", ErrQuit
	default:
		return fmt.Sprintf("unknown command %q — try .help", cmd), nil
	}
}
