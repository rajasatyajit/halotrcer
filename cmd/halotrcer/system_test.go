package main

import (
	"bytes"
	"testing"

	"github.com/rajasatyajit/halotrcer/internal/redact"
)

func TestLineRedactor(t *testing.T) {
	rd := redact.New(redact.Options{Home: "/home/canaryperson", Users: []string{"canaryperson"}, Secrets: []string{"sk-canary-0123456789"}})
	var out bytes.Buffer
	l := &lineRedactor{w: &out, rd: rd}
	for _, chunk := range []string{"==> Downloading https://u:p@h.example/x?tok", "en=1\n", "installed to /home/canary", "person/.local/bin\rkey sk-canary-01", "23456789 done"} {
		if _, err := l.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	l.Flush()
	want := "==> Downloading https://h.example/x\ninstalled to ~/.local/bin\rkey [redacted] done"
	if out.String() != want {
		t.Errorf("got  %q\nwant %q", out.String(), want)
	}
}
