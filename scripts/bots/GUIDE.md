# Setting up `hivemind bot`

This guide walks you through setting up `hivemind bot`, a small reference bot that ships with
hivemind itself. It exists so that anyone building or testing hivemind's Bot SDK and Slash
Commands feature has a real program to point at, instead of having to reach for a third-party
mock service. You do not need to write any code to use it — you only need to write small shell
scripts, which this guide will show you how to do.

## 1. What this is

`hivemind bot` has two jobs, and they map to the two halves of the Bot SDK described in
`internal-docs/SPEC.md` §4.12.

The first job is answering slash commands. When you run `hivemind bot` on its own, it starts a
small web server that stays running in your terminal. Whenever someone in hivemind types a slash
command like `/status` and presses Enter, hivemind sends a signed HTTP request to whatever web
address you registered for that command. `hivemind bot` is designed to be that address: it looks
at which command was called, runs a small shell script you provide for that command, and sends
the script's output back to hivemind in the format it expects. This is the piece that lets you
test every slash-command scenario — a normal reply, a reply that posts into the channel, a
script that fails on purpose, and a script that takes too long to respond — using real scripts
instead of a hand-rolled mock server.

The second job is posting a message on its own initiative, without waiting for anyone to type a
slash command. You run this with `hivemind bot post`, and it exits as soon as it has posted its
message — it is not a long-running process the way the listener is. This is useful for testing
the other half of the Bot SDK: a bot that announces something (a deploy finishing, a scheduled
report, and so on) using its own bearer token, the same way a real integration would.

## 2. Before you start

First, build the hivemind binary from the root of this repository:

```bash
make build
```

This produces `bin/hivemind`, which is the binary every command in this guide uses.

Second, if you plan to answer slash commands (job one above), there is one thing you need to
arrange yourself: a public HTTPS web address that forwards to the `hivemind bot` listener running
on your machine.

**Why can't I just point the webhook URL at `localhost`?** hivemind refuses to save a slash
command whose webhook address resolves to `localhost`, `127.0.0.1`, or anything else on a
loopback or private network — the exact same check it applies to outgoing webhooks. This isn't
an oversight; a slash command's webhook URL is something an admin can set to anything, and if
hivemind allowed loopback or private-network addresses, that URL could be pointed at hivemind's
own internal network — its own admin endpoints, other internal services, and so on — turning
hivemind itself into a tool for attacking its own infrastructure. hivemind has no way to tell
"this is a developer testing locally" apart from "this is an attack," so it blocks both the same
way, with no exception for local testing.

Because of that, `hivemind bot` needs a genuinely public address in front of it, even though it's
running on your own machine. A tunnel tool solves this by giving your local listener a real
public hostname that forwards straight back to your machine — the traffic still ends up on your
laptop, hivemind just can't (and shouldn't be able to) tell that from any other public host. A
common, free choice is [ngrok](https://ngrok.com/): once installed, running `ngrok http 8091`
(the listener's default port) prints a public `https://something.ngrok-free.app` address. Any
similar tunnel tool works the same way — the important part is that the address hivemind sees
must start with `https://` and must actually be reachable from the internet.

If you only plan to try `hivemind bot post` (job two above), you can skip the tunnel entirely —
posting a message never requires hivemind to reach back out to your machine.

**A tunnel-free alternative for local testing.** If you're running your own hivemind server
purely for local development — not a shared or production instance — you can start it with the
`--allow-insecure-webhooks` flag instead of setting up a tunnel:

```bash
./bin/hivemind serve --allow-insecure-webhooks
```

This tells that one server instance to skip the public-HTTPS requirement for webhook URLs, so you
can register a slash command's webhook URL as `http://localhost:8091/hooks/echo` directly and
skip the tunnel step entirely for the rest of this guide. hivemind prints a warning on startup
whenever this flag is on, as a reminder that it should never be used on a server anyone else can
reach — it exists purely so a solo local test setup doesn't need a tunnel. If in doubt, use the
tunnel instead; it works the same way against any hivemind server, local or not.

## 3. Setting up a bot in the graphical interface

Before you can register a slash command, you need a bot for it to post as (every slash command's
"in_channel" reply is posted using some bot's identity), and if you want to try `hivemind bot
post` you will also need that bot's bearer token. Both come from the same place: hivemind's
Settings page.

1. Log in to hivemind in your browser as an administrator — bot management is an admin-only
   area.
2. Click the profile block in the bottom-left corner of the sidebar to open its menu, then click
   **Settings**.
3. In the sidebar that appears, look under the **Workspace** section and click **Bots** — if you
   don't see this option, you are not logged in as an administrator.
4. Click the **New bot** button in the top-right corner. A small form appears asking for a name
   and a description; the name is what will show up next to messages this bot posts, and the
   description is just a note to help you remember what the bot is for.
5. Type something like `Test Bot` for the name and `hivemind bot reference implementation` for
   the description, then click **Create bot**.
6. The panel immediately changes to show you a bearer token starting with `hm_`. Read the
   warning above it carefully: this is the only time hivemind will ever show you this token in
   full — if you close this panel without copying it, your only option later is to regenerate a
   new one, which invalidates this one.
7. Click **Copy**, then paste the token somewhere safe for a moment (you will need it in step 7
   of this guide if you want to try `hivemind bot post`). Click **Done** to close the panel.
8. Confirm your new bot now appears in the Bots table, with its name, a small **BOT** badge, the
   description you typed, and a status of **Active**.

For the full, click-by-click detail of everything on this screen — including what the
Regenerate Token and Revoke buttons do — see Step 3 of
`internal-docs/walkthroughs/walkthrough-M23.md`; this guide only covers what you need to get
`hivemind bot` running.

## 4. Registering a slash command that points at `hivemind bot`

Now start your tunnel, if you haven't already, and keep note of the public HTTPS address it
gives you — for example `https://abcd1234.ngrok-free.app`. This guide will refer to that address
as your tunnel host. If you started hivemind with `--allow-insecure-webhooks` instead (see
step 2), your tunnel host is simply `http://localhost:8091` — the listener's own address — and
you can skip starting a tunnel altogether.

Still on the **Bots** page in Settings, look for the **Slash commands** section below the bot
list and click **New command**. A form appears with several fields; fill them in as follows:

- **Trigger** should be `/echo`. This must start with a slash, and it is what members will type
  in the composer to run this command.
- **Post as** should be the bot you created in step 3 — pick it from the dropdown.
- **Description** can be anything short, like `Echoes back what you typed`.
- **Syntax hint** can be left blank, or you can type something like `<message>` to remind people
  what to type after the trigger.
- **Webhook URL** is the important one: type your tunnel host followed by `/hooks/echo` — for
  example, `https://abcd1234.ngrok-free.app/hooks/echo`. The `/hooks/echo` part matters: it
  matches the name of one of the sample scripts that ships in this directory
  (`scripts/bots/commands/echo`), and `hivemind bot` uses that path to know which script to run.
- Leave **Restrict execution to owners/admins** unchecked for now — you can experiment with it
  later once the basic setup works.

Click **Create command**. Just like when you created the bot, hivemind will show you a signing
secret exactly once, starting with `whsec_`. This secret is how `hivemind bot` proves that a
request really came from your hivemind server and not from someone else — copy it now.

Save that secret into a file at `scripts/bots/commands/echo.secret`, in this same directory as
the `echo` script. For example, from the root of this repository:

```bash
echo "whsec_the_secret_you_just_copied" > scripts/bots/commands/echo.secret
```

The name of that file matters: it must be the trigger's name (without the slash) followed by
`.secret`, sitting right next to the script with the same name. `hivemind bot` looks for this
pairing automatically when it starts up, and a script without a matching `.secret` file is
quietly skipped rather than causing an error — so if a command doesn't seem to work, this file is
the first thing to check.

## 5. Running the listener

With the secret file in place, start the listener from the root of this repository:

```bash
./bin/hivemind bot
```

By default this listens on port 8091 and looks for scripts in `scripts/bots/commands`, which is
exactly where the sample scripts and the secret file you just created both live — so you do not
need to pass any flags for this basic setup. You should see log lines confirming it registered
the `echo` trigger (and a warning for any sample script that doesn't have a `.secret` file yet,
which is expected until you register more commands). Leave this running in its own terminal
window; it needs to stay up to answer requests.

Make sure your tunnel (`ngrok http 8091` or equivalent) is also still running, pointed at the
same port.

Now go back to hivemind in your browser, open any channel, and type `/echo hello world` into the
composer. As soon as you press Enter, you should see a small card appear labeled "Only visible to
you", containing the text `You said: hello world (from @yourusername)`. That confirms the whole
chain is working: hivemind signed the request, sent it to your tunnel, your tunnel forwarded it
to `hivemind bot`, the bot verified the signature and ran the `echo` script, and the script's
output came back and was rendered as an ephemeral card.

## 6. Trying the other sample scripts

Three more sample scripts ship alongside `echo`, each demonstrating a different scenario you'll
want to test. Register each one the same way you registered `/echo` in step 4 — same steps, just
a different trigger name and a webhook URL ending in that trigger's name instead of `/echo` — and
remember to save each command's secret to its own `.secret` file (`announce.secret`,
`fail.secret`, `slow.secret`).

**`/announce`** demonstrates a reply that posts into the channel for everyone to see, rather than
just to you. Register it with webhook URL ending in `/hooks/announce`. When you run
`/announce something happened`, instead of a private card, a normal message appears in the
channel, authored by the bot you picked — and if you have the channel open in a second browser
window logged in as someone else, you'll see it arrive there too, live, without needing to
refresh.

**`/fail`** demonstrates what happens when a script deliberately reports failure. Register it
with webhook URL ending in `/hooks/fail`. Running `/fail` always produces a private card
explaining that the script exited with a non-zero status, along with the message the script
printed — this is how you'll see a real bot's error messages if you ever write one that can fail.

**`/slow`** demonstrates hivemind's own timeout handling. Register it with webhook URL ending in
`/hooks/slow`. This script deliberately waits ten seconds before responding, which is longer than
the five seconds hivemind is willing to wait for any slash command. Running `/slow` will show a
brief "Executing…" indicator and then, after about five seconds, a card explaining that the
command timed out — this happens on hivemind's side, not inside `hivemind bot`, so it's a good
way to confirm that a slow or broken integration doesn't hang your composer forever.

If you'd like to write your own script instead of using the samples, put a new file in
`scripts/bots/commands/` (the file's name becomes the trigger), and remember that its contents
can use `{{.Username}}`, `{{.Args}}`, `{{.ArgsJoined}}`, `{{.ChannelID}}`, and `{{.Trigger}}` as
placeholders — `hivemind bot` fills these in from the actual request before running your script.
Whatever the script prints to standard output becomes the reply text; if it prints valid JSON
like `{"response_type":"in_channel","text":"..."}`, that exact shape is used instead, which is
how `announce` posts into the channel rather than replying privately.

## 7. Posting proactively

To try the other half of the Bot SDK — a bot posting on its own, without any slash command being
typed — use the bearer token you saved in step 3:

```bash
./bin/hivemind bot post \
  --token hm_the_token_you_copied_in_step_3 \
  --channel general \
  --command 'echo "Deploy of {{.Vars.branch}} finished successfully"' \
  --var branch=main
```

This renders the `--command` text as a template (filling in `{{.Vars.branch}}` from the `--var`
flag you passed), runs it, and posts whatever it printed as a new message in the `general`
channel, using the bot identity that token belongs to. `--channel` also accepts a numeric channel
ID if you prefer. This command exits as soon as it has posted the message — there is nothing left
running afterward.

## 8. Troubleshooting

Any card that says "That command failed to respond" or shows a generic error means hivemind's
own attempt to call your webhook didn't succeed — the ephemeral card itself never carries the
real reason, since it's shown to whoever ran the command, not just an admin. Two separate places
can tell you what actually went wrong, so check both:

- **The `hivemind serve` terminal (or its log file)** logs a line every time a slash-command
  webhook call fails, naming the trigger and the reason — `context deadline exceeded` (nothing
  answered in time), a connection error (`connection refused`, `no such host`, and so on), a
  non-2xx status code with the response body, or "response was not valid JSON" / "unrecognized
  response_type" if `hivemind bot` answered but with something malformed.
- **The `hivemind bot` terminal (or its log file)** logs every request it actually received,
  including ones it rejected for a bad signature. If a request never shows up here at all, the
  problem is upstream of `hivemind bot` entirely — the webhook call never arrived. That almost
  always points to the webhook URL itself: wrong port, wrong `/hooks/<trigger>` path, the tunnel
  or `hivemind bot` process not actually running, or (if you're not using
  `--allow-insecure-webhooks`) the tunnel address having changed since you registered it.

If `hivemind serve`'s log shows a connection error and `hivemind bot`'s log shows nothing, that
confirms the request never reached your machine — recheck the webhook URL and that both processes
are still running before looking any further.

**hivemind refuses to save my webhook URL.** This almost always means the address isn't a public
`https://` address, and the server wasn't started with `--allow-insecure-webhooks`. Double check
that you copied your tunnel's address exactly (including `https://`, not `http://`), and that the
tunnel is actually running — most tunnel tools print a fresh address every time you restart them,
so an old address you registered earlier will stop working the moment you restart the tunnel. If
you're intentionally using `http://localhost`, confirm the server was actually started with
`--allow-insecure-webhooks` — restarting it without that flag will bring the guard back.

**Running the slash command shows a generic failure card, and `hivemind bot`'s logs show
"rejected call with invalid signature".** The secret saved in the `.secret` file doesn't match
the one hivemind is signing with. This happens most often when a command was deleted and
re-created (which generates a brand new secret) without updating the `.secret` file to match, or
when the secret was copied with extra surrounding whitespace or a missing line. Regenerating the
command's secret from the Settings page and re-saving it into the `.secret` file resolves this.

**The card shows a timeout after about five seconds, but my script should be fast.** Check that
`hivemind bot` and your tunnel are both still running — if either one has stopped, hivemind will
wait the full five seconds for a response that never arrives and then show the same timeout card
it would show for a script that's genuinely too slow.
