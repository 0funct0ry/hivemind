package bot

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// maxRequestBody caps how much of a slash-command execution POST body the listener will read,
// as a basic defensive limit against a misbehaving or malicious caller.
const maxRequestBody = 1 << 20 // 1 MiB

// Options configures a Server.
type Options struct {
	Shell               string
	ScriptTimeout       time.Duration
	DefaultResponseType string
	MaxClockSkew        time.Duration
	Logger              *slog.Logger
}

// Server answers slash-command webhook calls, one route per discovered Route.
type Server struct {
	routes map[string]Route
	opts   Options
}

// NewServer builds a Server from the routes Discover found. A nil Logger falls back to
// slog.Default().
func NewServer(routes []Route, opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	byTrigger := make(map[string]Route, len(routes))
	for _, r := range routes {
		byTrigger[r.Trigger] = r
	}
	return &Server{routes: byTrigger, opts: opts}
}

// Handler returns the http.Handler serving POST /hooks/<trigger> for every discovered route.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for trigger, route := range s.routes {
		mux.HandleFunc("POST /hooks/"+trigger, s.handle(route))
	}
	return mux
}

func (s *Server) handle(route Route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log := s.opts.Logger.With("trigger", route.Trigger, "remote_addr", r.RemoteAddr)

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
		if err != nil {
			log.Warn("could not read request body", "error", err)
			http.Error(w, "request body too large or unreadable", http.StatusBadRequest)
			return
		}

		// Re-read the secret fresh on every call, not just at startup — otherwise pasting a
		// regenerated secret into the .secret file silently has no effect until the listener
		// is restarted, which is a confusing way to fail (a 401 with no clue why).
		secretBytes, err := os.ReadFile(route.SecretPath)
		if err != nil {
			log.Error("could not read secret file", "error", err)
			http.Error(w, "server misconfigured", http.StatusInternalServerError)
			return
		}
		secret := strings.TrimSpace(string(secretBytes))

		if !VerifySignature(secret, body, r.Header.Get("X-Hivemind-Signature")) {
			log.Warn("rejected call with invalid signature", "secret_path", route.SecretPath)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		if !VerifyTimestamp(r.Header.Get("X-Hivemind-Timestamp"), time.Now(), s.opts.MaxClockSkew) {
			log.Warn("rejected call with stale or missing timestamp")
			http.Error(w, "stale timestamp", http.StatusUnauthorized)
			return
		}

		var payload CommandExecPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Warn("could not decode payload", "error", err)
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		threadID := ""
		if payload.ThreadID != nil {
			threadID = *payload.ThreadID
		}
		data := TemplateData{
			Trigger:    "/" + route.Trigger,
			Args:       payload.Args,
			ArgsJoined: strings.Join(payload.Args, " "),
			UserID:     payload.UserID,
			Username:   payload.Username,
			ChannelID:  payload.ChannelID,
			ThreadID:   threadID,
			Time:       time.Now().Format(time.RFC3339),
			Hostname:   hostname(),
		}

		// Re-read the script fresh on every call so edits take effect without restarting the
		// listener — useful while iterating on a script during manual testing.
		content, err := os.ReadFile(route.ScriptPath)
		if err != nil {
			log.Error("could not read script", "error", err)
			writeJSON(w, CommandResponse{ResponseType: ResponseTypeEphemeral, Text: "bot: could not read script: " + err.Error()})
			return
		}

		rendered, err := Render(string(content), data)
		if err != nil {
			log.Error("template render failed", "error", err)
			writeJSON(w, CommandResponse{ResponseType: ResponseTypeEphemeral, Text: "bot: template error: " + err.Error()})
			return
		}

		stdout, stderr, exitCode, runErr := Run(r.Context(), s.opts.Shell, rendered, s.opts.ScriptTimeout)
		resp := BuildResponse(stdout, stderr, exitCode, runErr, s.opts.DefaultResponseType)

		log.Info("handled slash command", "exit_code", exitCode, "response_type", resp.ResponseType, "latency_ms", time.Since(start).Milliseconds())
		writeJSON(w, resp)
	}
}

func writeJSON(w http.ResponseWriter, resp CommandResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}
