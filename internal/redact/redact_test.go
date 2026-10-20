package redact

import (
	"strings"
	"testing"
)

func TestURL(t *testing.T) {
	cases := map[string]string{
		"https://api.example.com/v1":                                              "https://api.example.com/v1",
		"https://user:pass@api.example.com/v1?api_key=sk-123#frag":                "https://api.example.com/v1",
		"http://127.0.0.1:8089/v1/":                                               "http://127.0.0.1:8089/v1/",
		"https://mcp.example.com/server/123e4567-e89b-12d3-a456-426614174000/mcp": "https://mcp.example.com/server/{masked:36}/mcp",
		"https://hooks.example.com/T00000000/B00000000/abcdefghij1234567890XY":    "https://hooks.example.com/T00000000/B00000000/{masked:22}",
		"https://example.com/docs/getting-started-with-the-product":               "https://example.com/docs/getting-started-with-the-product",
		"not a url": "not a url",
	}
	for in, want := range cases {
		if got := URL(in); got != want {
			t.Errorf("URL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStringHomeUserSecrets(t *testing.T) {
	r := New(Options{
		Home:    "/home/fixtureuser",
		Users:   []string{"fixtureuser", "root", "x"},
		Secrets: []string{"sk-canary-0123456789abcdef", "short"},
	})
	cases := map[string]string{
		"/home/fixtureuser/.config/tokvera/config.yaml": "~/.config/tokvera/config.yaml",
		"/home/fixtureuser":                             "~",
		"/home/fixtureuserX/file":                       "/home/fixtureuserX/file", // not the home dir, and not the whole word
		"at /tmp/-home-fixtureuser-work/x":              "at /tmp/-home-<user>-work/x",
		"FixtureUser ran it":                            "<user> ran it",
		"fixtureusers":                                  "fixtureusers",
		"root and x stay":                               "root and x stay",
		"key sk-canary-0123456789abcdef used":           "key [redacted] used",
		"short stays":                                   "short stays", // below minSecretLen
		"see https://u:p@h.example/p?token=abc.":        "see https://h.example/p.",
		`{"path":"/home/fixtureuser/a"}`:                `{"path":"~/a"}`,
	}
	for in, want := range cases {
		if got := r.String(in); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWindowsHome(t *testing.T) {
	r := New(Options{Home: `C:\Users\Fixture`, Users: []string{"Fixture"}, FoldCase: true})
	for in, want := range map[string]string{
		`C:\Users\Fixture\AppData\Roaming\Claude\claude_desktop_config.json`: `~\AppData\Roaming\Claude\claude_desktop_config.json`,
		`c:\users\fixture\x`: `~\x`,
		`C:/Users/Fixture/x`: `~/x`,
	} {
		if got := r.String(in); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFromEnvironment(t *testing.T) {
	t.Setenv("HOME", "/home/canaryperson")
	t.Setenv("USERPROFILE", `C:\Users\canaryperson`)
	t.Setenv("USER", "canaryperson")
	t.Setenv("MY_API_TOKEN", "tok-canary-abcdefgh12345678")
	t.Setenv("PLAIN_VALUE", "not-secret-but-named-later")
	t.Setenv("PLAIN_SETTING", "visible-value-123")
	r := New(FromEnvironment("PLAIN_VALUE"))
	in := "canaryperson used tok-canary-abcdefgh12345678 and not-secret-but-named-later and visible-value-123"
	out := r.String(in)
	if l := Leaks(out, "canaryperson", "tok-canary-abcdefgh12345678", "not-secret-but-named-later"); len(l) > 0 {
		t.Errorf("leaked %v in %q", l, out)
	}
	if !strings.Contains(out, "visible-value-123") {
		t.Errorf("over-redacted: %q", out)
	}
}

func TestNilAndZero(t *testing.T) {
	var r *Redactor
	if r.String("x") != "x" {
		t.Error("nil redactor changed text")
	}
	z := New(Options{})
	if got := z.String("https://a:b@h.example/?q=1 /home/x"); got != "https://h.example/ /home/x" {
		t.Errorf("zero redactor: %q", got)
	}
}
