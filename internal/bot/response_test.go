package bot

import (
	"errors"
	"testing"
)

func TestBuildResponse(t *testing.T) {
	cases := []struct {
		name     string
		stdout   string
		stderr   string
		exitCode int
		runErr   error
		want     CommandResponse
	}{
		{
			name:   "ephemeral JSON passthrough",
			stdout: `{"response_type":"ephemeral","text":"ok"}`,
			want:   CommandResponse{ResponseType: "ephemeral", Text: "ok"},
		},
		{
			name:   "in_channel JSON passthrough",
			stdout: `{"response_type":"in_channel","text":"deployed"}`,
			want:   CommandResponse{ResponseType: "in_channel", Text: "deployed"},
		},
		{
			name:   "plain text wrapped with default type",
			stdout: "hello there",
			want:   CommandResponse{ResponseType: "ephemeral", Text: "hello there"},
		},
		{
			name:   "JSON missing a recognized response_type is wrapped as plain text",
			stdout: `{"foo":"bar"}`,
			want:   CommandResponse{ResponseType: "ephemeral", Text: `{"foo":"bar"}`},
		},
		{
			name:     "non-zero exit forces ephemeral regardless of stdout",
			stdout:   `{"response_type":"in_channel","text":"should be ignored"}`,
			stderr:   "boom",
			exitCode: 1,
			want:     CommandResponse{ResponseType: "ephemeral", Text: "script exited 1: boom"},
		},
		{
			name:   "run error forces ephemeral with the error text",
			runErr: errors.New("script exceeded its 10s timeout"),
			want:   CommandResponse{ResponseType: "ephemeral", Text: "script exceeded its 10s timeout"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildResponse([]byte(tc.stdout), []byte(tc.stderr), tc.exitCode, tc.runErr, "ephemeral")
			if got.ResponseType != tc.want.ResponseType || got.Text != tc.want.Text {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestBuildResponseDefaultTypeInChannel(t *testing.T) {
	got := BuildResponse([]byte("hi"), nil, 0, nil, "in_channel")
	if got.ResponseType != "in_channel" || got.Text != "hi" {
		t.Fatalf("got %+v", got)
	}
}

func TestBuildResponseInvalidDefaultFallsBackToEphemeral(t *testing.T) {
	got := BuildResponse([]byte("hi"), nil, 0, nil, "bogus")
	if got.ResponseType != ResponseTypeEphemeral {
		t.Fatalf("expected an invalid default type to fall back to ephemeral, got %+v", got)
	}
}
