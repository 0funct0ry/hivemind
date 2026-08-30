package chatclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client is a REST client for the hivemind API, wrapping *http.Client with a base URL and a
// bearer token. It never touches the store directly — every method is a plain HTTP call.
type Client struct {
	hc      *http.Client
	baseURL string
	token   string
}

// New constructs a Client against baseURL (e.g. "https://localhost:8080"). When insecure is
// true, TLS certificate verification is skipped — the client only ever consumes TLS, never
// terminates it.
func New(baseURL string, insecure bool) *Client {
	transport := &http.Transport{}
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit --insecure opt-in
	}
	return &Client{hc: &http.Client{Transport: transport, Timeout: 30 * time.Second}, baseURL: baseURL}
}

// BaseURL returns the client's configured base URL.
func (c *Client) BaseURL() string { return c.baseURL }

// SetToken sets the bearer token used on every subsequent request.
func (c *Client) SetToken(token string) { c.token = token }

// Token returns the currently configured bearer token, for callers (like the WS dialer) that
// need to authenticate the same way REST calls do.
func (c *Client) Token() string { return c.token }

func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/api/v1"+path, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response %s %s: %w", method, path, err)
	}
	if resp.StatusCode >= 300 {
		var e apiError
		_ = json.Unmarshal(respBody, &e)
		if e.Error.Code == "" {
			e.Error.Code = "unknown_error"
			e.Error.Message = fmt.Sprintf("request failed with status %d", resp.StatusCode)
		}
		return &APIError{StatusCode: resp.StatusCode, Code: e.Error.Code, Message: e.Error.Message, Field: e.Error.Field}
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response %s %s: %w", method, path, err)
	}
	return nil
}

// Login authenticates with username/password and returns the hm_session cookie the server
// sets — it is used only to immediately mint a bearer token (IssueToken) and then discarded,
// per SPEC.md §7.7.
func (c *Client) Login(ctx context.Context, username, password string) (*http.Cookie, error) {
	body := map[string]string{"login": username, "password": password}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode login request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/auth/login", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("build login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var e apiError
		_ = json.Unmarshal(respBody, &e)
		if e.Error.Code == "" {
			e.Error.Code = "unauthenticated"
			e.Error.Message = "Invalid login or password."
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Code: e.Error.Code, Message: e.Error.Message, Field: e.Error.Field}
	}
	for _, ck := range resp.Cookies() {
		if ck.Name == "hm_session" {
			return ck, nil
		}
	}
	return nil, fmt.Errorf("login succeeded but no session cookie was set")
}

// IssueToken mints a bearer token using a session cookie obtained from Login. This always
// mints a "cli_session" purposed token (SPEC.md §7.7's login-then-mint flow) — the CLI
// equivalent of a browser session cookie, kept out of the user's self-service API-keys list
// and surfaced only in the admin Sessions view instead. expiresIn of zero means the token
// never expires.
func (c *Client) IssueToken(ctx context.Context, cookie *http.Cookie, name string, expiresIn time.Duration) (string, error) {
	body := map[string]string{"name": name, "purpose": "cli_session"}
	if expiresIn > 0 {
		body["expires_in"] = expiresIn.String()
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode token request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/tokens", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// A session-cookie request must satisfy the server's CSRF check (RequireAuth's
	// csrfOK, internal/api/router.go), which requires Origin (or Referer) to match the
	// request's own host — bearer-token requests skip this, but IssueToken is the one
	// call in this client that still authenticates via the cookie Login returned.
	req.Header.Set("Origin", c.baseURL)
	req.AddCookie(cookie)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("issue token: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var e apiError
		_ = json.Unmarshal(respBody, &e)
		return "", &APIError{StatusCode: resp.StatusCode, Code: e.Error.Code, Message: e.Error.Message, Field: e.Error.Field}
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	return out.Token, nil
}

// ListChannels returns every channel the caller can see.
func (c *Client) ListChannels(ctx context.Context) ([]Channel, error) {
	var out struct {
		Data []Channel `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/channels", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// GetMessages fetches one page of a channel's root messages.
func (c *Client) GetMessages(ctx context.Context, channelID string, after, before *string, limit int) (MessagePage, error) {
	q := url.Values{}
	if after != nil {
		q.Set("after", *after)
	}
	if before != nil {
		q.Set("before", *before)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out MessagePage
	path := "/channels/" + url.PathEscape(channelID) + "/messages"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return MessagePage{}, err
	}
	return out, nil
}

// PostMessage sends a message via REST — the only path messages are ever sent over, per
// SPEC.md §5.4 ("Messages are never sent over the WebSocket"). Passing the same clientMsgID
// on retry makes this call idempotent (SPEC.md §4.1).
func (c *Client) PostMessage(ctx context.Context, channelID, body, clientMsgID string, threadID *string) (Message, error) {
	in := map[string]any{"body": body}
	if clientMsgID != "" {
		in["client_msg_id"] = clientMsgID
	}
	if threadID != nil {
		in["thread_id"] = *threadID
	}
	var out struct {
		Message Message `json:"message"`
	}
	path := "/channels/" + url.PathEscape(channelID) + "/messages"
	if err := c.doJSON(ctx, http.MethodPost, path, in, &out); err != nil {
		return Message{}, err
	}
	return out.Message, nil
}

// Me returns the currently authenticated user.
func (c *Client) Me(ctx context.Context) (User, error) {
	var out struct {
		User User `json:"user"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/auth/me", nil, &out); err != nil {
		return User{}, err
	}
	return out.User, nil
}

// Members lists a channel's members.
func (c *Client) Members(ctx context.Context, channelID string) ([]User, error) {
	var out struct {
		Data []User `json:"data"`
	}
	path := "/channels/" + url.PathEscape(channelID) + "/members"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// ListUsers searches every user in the workspace by username/display-name prefix (q may be
// empty to list everyone, up to limit).
func (c *Client) ListUsers(ctx context.Context, q string, limit int) ([]User, error) {
	query := url.Values{}
	if q != "" {
		query.Set("q", q)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	path := "/users"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var out struct {
		Data []User `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// ListDMs lists the caller's DM and group DM channels, peer/members inlined.
func (c *Client) ListDMs(ctx context.Context) ([]DM, error) {
	var out struct {
		Data []DM `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/dms", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// CreateDM gets-or-creates a DM (one id) or group DM (2+ ids) with the given user ids.
func (c *Client) CreateDM(ctx context.Context, userIDs []string) (DM, error) {
	var out struct {
		Channel DM `json:"channel"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/dms", map[string]any{"user_ids": userIDs}, &out); err != nil {
		return DM{}, err
	}
	return out.Channel, nil
}
