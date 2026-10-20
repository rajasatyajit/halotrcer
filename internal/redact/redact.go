// Package redact is Halotrcer's own scrubber, applied to every string it
// renders or writes, on top of the masking both tools already do (which it
// never undoes). PLAN.md §4:
//   - URLs keep scheme, host and path; userinfo, query and fragment are
//     dropped and token-like path segments masked.
//   - Values of secret-named environment variables (and any the caller
//     names, such as the --api-key-env variable) never appear.
//   - Paths under the home directory are shown from ~.
//   - The user's name becomes <user>, as whole words in any case, with the
//     same system-name exemptions as Sievant's report (sievant
//     pkg/report/privacy.go).
package redact

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode"
)

// Options configure a Redactor; FromEnvironment fills them from the process.
type Options struct {
	Home string
	// Users are names to replace with <user>.
	Users []string
	// Secrets are literal values that must never appear.
	Secrets []string
	// FoldCase compares home paths case-insensitively (Windows).
	FoldCase bool
}

// Redactor scrubs strings. The zero value only cleans URLs.
type Redactor struct {
	homes    []string
	foldCase bool
	userRE   *regexp.Regexp
	secrets  []string
}

// Names a user name may take that are not personal: never replaced.
var systemNames = map[string]bool{
	"root": true, "admin": true, "administrator": true, "user": true,
	"runner": true, "home": true, "nobody": true,
}

// secretNameRE matches environment variable names whose values are treated
// as secrets.
var secretNameRE = regexp.MustCompile(`(?i)(key|token|secret|passw|pwd|auth|credential|cookie|session|bearer)`)

// notSecret are variables whose names match secretNameRE but hold the
// shell's working directory, sockets or session plumbing, not credentials:
// scrubbing their values would hide ordinary paths.
var notSecret = map[string]bool{
	"PWD": true, "OLDPWD": true, "SSH_AUTH_SOCK": true, "XAUTHORITY": true, "DBUS_SESSION_BUS_ADDRESS": true,
}

// minSecretLen keeps short, common values (such as "1" or "true") from
// being scrubbed out of unrelated text.
const minSecretLen = 8

// FromEnvironment builds Options from the running process: the home
// directory, the user names in HOME's basename, USER, USERNAME and LOGNAME,
// and the value of every secret-named environment variable plus the
// variables named in extraSecretVars.
func FromEnvironment(extraSecretVars ...string) Options {
	o := Options{FoldCase: runtime.GOOS == "windows"}
	if h, err := os.UserHomeDir(); err == nil {
		o.Home = h
		o.Users = append(o.Users, filepath.Base(h))
	}
	for _, k := range []string{"USER", "USERNAME", "LOGNAME"} {
		if v := os.Getenv(k); v != "" {
			o.Users = append(o.Users, v)
		}
	}
	extra := map[string]bool{}
	for _, k := range extraSecretVars {
		extra[k] = true
	}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || v == "" {
			continue
		}
		if extra[k] || (secretNameRE.MatchString(k) && !notSecret[k] && !strings.HasPrefix(k, "XDG_")) {
			o.Secrets = append(o.Secrets, v)
		}
	}
	return o
}

// New builds a Redactor.
func New(o Options) *Redactor {
	r := &Redactor{foldCase: o.FoldCase}
	if h := strings.TrimRight(o.Home, `/\`); len(h) > 1 {
		r.homes = append(r.homes, h)
		if alt := strings.ReplaceAll(h, `\`, "/"); alt != h {
			r.homes = append(r.homes, alt)
		}
	}
	var users []string
	seen := map[string]bool{}
	for _, u := range o.Users {
		l := strings.ToLower(strings.TrimSpace(u))
		if len(l) < 2 || systemNames[l] || seen[l] {
			continue
		}
		seen[l] = true
		users = append(users, regexp.QuoteMeta(l))
	}
	if len(users) > 0 {
		sort.Slice(users, func(i, j int) bool { return len(users[i]) > len(users[j]) })
		// Whole words: not preceded or followed by a letter, digit or underscore.
		r.userRE = regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}_])(` + strings.Join(users, "|") + `)($|[^\p{L}\p{N}_])`)
	}
	for _, s := range o.Secrets {
		if len(s) >= minSecretLen {
			r.secrets = append(r.secrets, s)
		}
	}
	// Longest first, so a secret containing another is replaced whole.
	sort.Slice(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
	return r
}

var urlRE = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'<>()\[\]{}]+`)

// String scrubs free text.
func (r *Redactor) String(s string) string {
	if r == nil {
		return s
	}
	for _, sec := range r.secrets {
		s = strings.ReplaceAll(s, sec, "[redacted]")
	}
	s = urlRE.ReplaceAllStringFunc(s, func(u string) string {
		trail := ""
		for strings.HasSuffix(u, ".") || strings.HasSuffix(u, ",") || strings.HasSuffix(u, ";") || strings.HasSuffix(u, ":") {
			trail = u[len(u)-1:] + trail
			u = u[:len(u)-1]
		}
		return URL(u) + trail
	})
	s = r.homeToTilde(s)
	if r.userRE != nil {
		// Two passes: adjacent matches share a boundary character.
		for range 2 {
			s = r.userRE.ReplaceAllString(s, "${1}<user>${3}")
		}
	}
	return s
}

// Path scrubs a filesystem path (the same rules as String).
func (r *Redactor) Path(p string) string { return r.String(p) }

func (r *Redactor) homeToTilde(s string) string {
	for _, h := range r.homes {
		if !r.foldCase {
			s = replacePrefixBounded(s, h, h)
			continue
		}
		s = replacePrefixBounded(s, h, strings.ToLower(h))
	}
	return s
}

// replacePrefixBounded replaces home with "~" wherever it appears followed by
// a separator, a quote or the end of the string, so /home/al does not eat
// /home/alice.
func replacePrefixBounded(s, home, needle string) string {
	hay := s
	if needle != home {
		hay = strings.ToLower(s)
	}
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(hay[i:], needle)
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		j += i
		end := j + len(needle)
		if end == len(s) || strings.ContainsRune(`/\"' `, rune(s[end])) {
			b.WriteString(s[i:j])
			b.WriteString("~")
		} else {
			b.WriteString(s[i:end])
		}
		i = end
	}
}

// URL keeps a URL's scheme, host and path, drops userinfo, query and
// fragment, and masks token-like path segments as {masked:N}. Anything that
// does not parse is masked whole.
func URL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		if strings.Contains(raw, "://") {
			return "{masked-url}"
		}
		return raw
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	segs := strings.Split(u.EscapedPath(), "/")
	for i, seg := range segs {
		if tokenLike(seg) {
			segs[i] = "{masked:" + itoa(len(seg)) + "}"
		}
	}
	out := u.Scheme + "://" + u.Host + strings.Join(segs, "/")
	return out
}

var uuidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// tokenLike reports whether a path segment looks like a credential: a UUID,
// a long hex string, or a long run mixing letters and digits.
func tokenLike(seg string) bool {
	if uuidRE.MatchString(seg) {
		return true
	}
	if len(seg) < 20 {
		return false
	}
	var letters, digits, other int
	for _, c := range seg {
		switch {
		case unicode.IsLetter(c):
			letters++
		case unicode.IsDigit(c):
			digits++
		case c == '-' || c == '_' || c == '.' || c == '~' || c == '%':
			other++
		default:
			return false
		}
	}
	return letters > 0 && digits > 0
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Leaks returns the canaries that occur in text (case-insensitively), for
// tests that plant secrets and check every output.
func Leaks(text string, canaries ...string) []string {
	var found []string
	l := strings.ToLower(text)
	for _, c := range canaries {
		if c != "" && strings.Contains(l, strings.ToLower(c)) {
			found = append(found, c)
		}
	}
	return found
}
