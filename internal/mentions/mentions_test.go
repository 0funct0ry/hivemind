package mentions

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []Ref
	}{
		{
			name: "single user mention",
			body: "Hello @alice!",
			want: []Ref{
				{Kind: "user", Name: "alice", Start: 6, End: 12},
			},
		},
		{
			name: "consecutive mentions",
			body: "@alice @bob @channel",
			want: []Ref{
				{Kind: "user", Name: "alice", Start: 0, End: 6},
				{Kind: "user", Name: "bob", Start: 7, End: 11},
				{Kind: "channel", Name: "channel", Start: 12, End: 20},
			},
		},
		{
			name: "mention at start and end",
			body: "@alice hello @bob",
			want: []Ref{
				{Kind: "user", Name: "alice", Start: 0, End: 6},
				{Kind: "user", Name: "bob", Start: 13, End: 17},
			},
		},
		{
			name: "only here",
			body: "@here",
			want: []Ref{
				{Kind: "here", Name: "here", Start: 0, End: 5},
			},
		},
		{
			name: "trailing punctuation",
			body: "Hey @priya, did you see @bob.smith? Or @charlie-?",
			want: []Ref{
				{Kind: "user", Name: "priya", Start: 4, End: 10},
				{Kind: "user", Name: "bob.smith", Start: 24, End: 34},
				{Kind: "user", Name: "charlie", Start: 39, End: 47},
			},
		},
		{
			name: "email address should not match",
			body: "contact foo@bar.com for details",
			want: nil,
		},
		{
			name: "fenced code blocks",
			body: "Check this out:\n```\n@alice is coding here\n```\nBut @bob is not.",
			want: []Ref{
				{Kind: "user", Name: "bob", Start: 50, End: 54},
			},
		},
		{
			name: "inline code spans",
			body: "Use `@alice` to mention her, or @bob directly.",
			want: []Ref{
				{Kind: "user", Name: "bob", Start: 32, End: 36},
			},
		},
		{
			name: "markdown link URLs containing @",
			body: "See [Alice's Profile](https://github.com/@alice) or @bob.",
			want: []Ref{
				{Kind: "user", Name: "bob", Start: 52, End: 56},
			},
		},
		{
			name: "raw URLs containing @",
			body: "Go to https://example.com/@alice/status/123 or mention @bob.",
			want: []Ref{
				{Kind: "user", Name: "bob", Start: 55, End: 59},
			},
		},
		{
			name: "unicode adjacent",
			body: "こんにちは@aliceさん",
			want: []Ref{
				{Kind: "user", Name: "alice", Start: 15, End: 21},
			},
		},
		{
			name: "invalid username too short",
			body: "Hello @a",
			want: nil,
		},
		{
			name: "invalid username starts with punctuation",
			body: "Hello @.alice",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.body)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMask(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "fenced code",
			body: "```go\n@alice\n```",
			want: "     \n      \n   ",
		},
		{
			name: "inline code",
			body: "hello `@alice` world",
			want: "hello          world",
		},
		{
			name: "markdown link",
			body: "[link](http://@alice)",
			want: "[link]               ",
		},
		{
			name: "raw url",
			body: "visit http://@alice now",
			want: "visit               now",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Mask(tt.body)
			if got != tt.want {
				t.Errorf("Mask() = %q, want %q", got, tt.want)
			}
		})
	}
}
