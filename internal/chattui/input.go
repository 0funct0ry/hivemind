package chattui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ergochat/readline"
)

// DefaultHistoryPath returns ~/.hivemind_chat_history, distinct from the admin shell's
// ~/.hivemind_history (SPEC.md §7.7).
func DefaultHistoryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".hivemind_chat_history"), nil
}

// completionTimeout bounds how long a Tab press will block on a network round-trip for
// dynamic argument completion (channel/user names) — long enough for a local server, short
// enough that a hung connection doesn't make typing feel broken.
const completionTimeout = 2 * time.Second

// dotCommandCompleter builds a Tab-completer offering every dot-command name in Commands,
// plus dynamic argument completion for the three commands that take one: .join (channel
// names), .dm (usernames), and .thread (tags currently assigned in the open view). The
// argument lists are fetched fresh from the server on each Tab press rather than cached, since
// channels/users can change between presses and the round-trip to a local server is fast
// enough not to matter.
func dotCommandCompleter(app *App) readline.AutoCompleter {
	items := make([]*readline.PrefixCompleter, len(Commands))
	for i, c := range Commands {
		switch c.Name {
		case ".join":
			items[i] = readline.PcItem(c.Name, readline.PcItemDynamic(channelNameCompletions(app)))
		case ".dm":
			items[i] = readline.PcItem(c.Name, readline.PcItemDynamic(usernameCompletions(app)))
		case ".thread":
			items[i] = readline.PcItem(c.Name, readline.PcItemDynamic(tagCompletions(app)))
		default:
			items[i] = readline.PcItem(c.Name)
		}
	}
	return readline.NewPrefixCompleter(items...)
}

func channelNameCompletions(app *App) func(string) []string {
	return func(string) []string {
		ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
		defer cancel()
		channels, err := app.Client.ListChannels(ctx)
		if err != nil {
			return nil
		}
		names := make([]string, 0, len(channels))
		for _, ch := range channels {
			if ch.Kind != "public" && ch.Kind != "private" {
				continue
			}
			if ch.Slug != nil && *ch.Slug != "" {
				names = append(names, *ch.Slug)
			} else {
				names = append(names, ch.Name)
			}
		}
		return names
	}
}

func usernameCompletions(app *App) func(string) []string {
	return func(string) []string {
		ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
		defer cancel()
		users, err := app.Client.ListUsers(ctx, "", 200)
		if err != nil {
			return nil
		}
		names := make([]string, 0, len(users))
		for _, u := range users {
			names = append(names, u.Username)
		}
		return names
	}
}

func tagCompletions(app *App) func(string) []string {
	return func(string) []string {
		return app.Tags.Keys()
	}
}

// NewInput constructs a readline instance configured with history at historyPath and Tab
// completion (including dynamic channel/user/tag completion via app) over the dot-commands.
// This is the first real consumer of the ergochat/readline dependency (approved but previously
// unused, SPEC.md §2.5) — it gives Up-Arrow history recall and standard line editing for free.
func NewInput(historyPath string, app *App) (*readline.Instance, error) {
	rl, err := readline.NewFromConfig(&readline.Config{
		Prompt:       "> ",
		HistoryFile:  historyPath,
		AutoComplete: dotCommandCompleter(app),
	})
	if err != nil {
		return nil, fmt.Errorf("create input: %w", err)
	}
	return rl, nil
}
