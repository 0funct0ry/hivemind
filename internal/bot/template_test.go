package bot

import "testing"

func TestRender(t *testing.T) {
	data := TemplateData{
		Trigger:    "/status",
		Args:       []string{"prod", "main"},
		ArgsJoined: "prod main",
		Username:   "priya",
		Vars:       map[string]string{"env": "production"},
	}

	got, err := Render(`{{.Trigger}} by {{.Username}}: {{join .Args ","}} ({{.Vars.env}})`, data)
	if err != nil {
		t.Fatal(err)
	}
	want := "/status by priya: prod,main (production)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderBadSyntaxErrors(t *testing.T) {
	if _, err := Render(`{{.Unclosed`, TemplateData{}); err == nil {
		t.Fatal("expected a parse error for malformed template syntax")
	}
}
