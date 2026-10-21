// Command mirror serves the smoke matrix's release mirror on loopback (PLAN.md
// M4): the GitHub layout the install scripts and `halotrcer install` read
// through HALOTRCER_INSTALL_DOWNLOAD_BASE and HALOTRCER_INSTALL_API_BASE, for
// every repository the snapshot holds, and the signed apt and yum
// repositories.
//
//	mirror --dist DIR [--url-file FILE] [--addr 127.0.0.1:0] [--latest REPO=TAG ...]
//
// DIR is scripts/smoke/snapshot.sh's output:
//
//	DIR/releases/<repo>/<tag>/<file>   served at /dl/<repo>/releases/download/<tag>/<file>
//	                                   and listed at /api/<repo>/releases[/latest]
//	DIR/repo/...                       served at /repo/...
//
// "latest" is the highest stable tag (GitHub's latest skips prereleases),
// unless --latest names it: a snapshot's tag (v0.0.1-snapshot-<commit>) looks
// like a prerelease, and stands for the latest release in the smoke matrix.
// A tag with a "-" is listed as a prerelease. The listening URL is written to FILE once
// the server accepts connections. Every request is logged on stderr.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var tagRE = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.-]+)?$`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mirror:", err)
		os.Exit(1)
	}
}

func run() error {
	dist := flag.String("dist", "", "the snapshot directory")
	urlFile := flag.String("url-file", "", "write the listening URL here")
	addr := flag.String("addr", "127.0.0.1:0", "the address to listen on (loopback)")
	latest := map[string]string{}
	flag.Func("latest", "REPO=TAG: the tag /api/REPO/releases/latest names (repeatable)", func(v string) error {
		repo, tag, ok := strings.Cut(v, "=")
		if !ok || !safe(repo) || !tagRE.MatchString(tag) {
			return fmt.Errorf("--latest %q: want REPO=vX.Y.Z[-pre]", v)
		}
		latest[repo] = tag
		return nil
	})
	flag.Parse()
	if *dist == "" {
		return errors.New("--dist is required")
	}
	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", *addr)
	if err != nil {
		return err
	}
	url := "http://" + ln.Addr().String()
	if *urlFile != "" {
		if err := os.WriteFile(*urlFile, []byte(url+"\n"), 0o600); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "mirror: serving %s at %s\n", *dist, url)
	srv := &http.Server{Handler: handler(*dist, latest), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func handler(dist string, latest map[string]string) http.Handler {
	releases := filepath.Join(dist, "releases")
	mux := http.NewServeMux()
	mux.Handle("/repo/", http.StripPrefix("/repo/", http.FileServer(http.Dir(filepath.Join(dist, "repo")))))
	mux.HandleFunc("/dl/{repo}/releases/download/{tag}/{file}", func(w http.ResponseWriter, r *http.Request) {
		repo, tag, file := r.PathValue("repo"), r.PathValue("tag"), r.PathValue("file")
		if !safe(repo) || !tagRE.MatchString(tag) || !safe(file) {
			http.NotFound(w, r)
			return
		}
		// Each element is checked above: one name, no separator, no "..".
		http.ServeFile(w, r, filepath.Join(releases, repo, tag, file)) //nolint:gosec // G703: see above
	})
	mux.HandleFunc("/api/{repo}/releases", func(w http.ResponseWriter, r *http.Request) {
		tags, ok := list(w, r, releases)
		if !ok {
			return
		}
		out := make([]map[string]any, 0, len(tags))
		for _, t := range tags {
			out = append(out, map[string]any{"tag_name": t, "prerelease": strings.Contains(t, "-"), "draft": false})
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("/api/{repo}/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		tags, ok := list(w, r, releases)
		if !ok {
			return
		}
		if t, ok := latest[r.PathValue("repo")]; ok {
			writeJSON(w, map[string]any{"tag_name": t})
			return
		}
		for _, t := range tags {
			if !strings.Contains(t, "-") {
				writeJSON(w, map[string]any{"tag_name": t})
				return
			}
		}
		http.NotFound(w, r)
	})
	return logged(mux)
}

// list is the repository's tags, newest first, or a 404.
func list(w http.ResponseWriter, r *http.Request, releases string) ([]string, bool) {
	repo := r.PathValue("repo")
	if !safe(repo) {
		http.NotFound(w, r)
		return nil, false
	}
	entries, err := os.ReadDir(filepath.Join(releases, repo))
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}
	var tags []string
	for _, e := range entries {
		if e.IsDir() && tagRE.MatchString(e.Name()) {
			tags = append(tags, e.Name())
		}
	}
	sort.Slice(tags, func(i, j int) bool { return newer(tags[i], tags[j]) })
	return tags, true
}

// newer orders tags by major.minor.patch, a release before its prereleases.
func newer(a, b string) bool {
	ma, mb := tagRE.FindStringSubmatch(a), tagRE.FindStringSubmatch(b)
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(ma[i])
		y, _ := strconv.Atoi(mb[i])
		if x != y {
			return x > y
		}
	}
	if (ma[4] == "") != (mb[4] == "") {
		return ma[4] == ""
	}
	return ma[4] > mb[4]
}

// safe is one path element: no separators, no dot-dot.
func safe(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func logged(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(os.Stderr, "mirror: %s %s\n", r.Method, r.URL.Path)
		h.ServeHTTP(w, r)
	})
}
