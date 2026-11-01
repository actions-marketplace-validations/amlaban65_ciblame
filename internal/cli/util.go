package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amlaban65/ciblame/internal/collect"
)

// parseArgs parses flags that may be interleaved with positional arguments
// ("ciblame acme/web --since 7d"), which the standard flag package does not
// allow on its own.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			// The flag package has already printed the error and usage.
			return nil, &usageError{msg: err.Error(), silent: true}
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		if args[0] == "--" {
			return append(pos, args[1:]...), nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// stringList is a repeatable string flag.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

var relative = regexp.MustCompile(`^(\d+)\s*([hdw])$`)

// parseWhen accepts "7d", "12h", "2w", "2026-10-01" or an RFC 3339 timestamp.
func parseWhen(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if m := relative.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		unit := map[string]time.Duration{"h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}[m[2]]
		return now.Add(-time.Duration(n) * unit), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, strings.ToUpper(s)); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q: use 7d, 12h, 2w, 2026-10-01 or an RFC 3339 timestamp", s)
}

var repoRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)

// normalizeRepo accepts "owner/name" or a github.com URL.
func normalizeRepo(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") || strings.HasPrefix(s, "github.com/") {
		if !strings.Contains(s, "://") {
			s = "https://" + s
		}
		u, err := url.Parse(s)
		if err != nil || !strings.EqualFold(u.Host, "github.com") {
			return "", fmt.Errorf("invalid repository %q", s)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 {
			return "", fmt.Errorf("invalid repository %q", s)
		}
		s = parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
	}
	if !repoRe.MatchString(s) || strings.Contains(s, "..") {
		return "", fmt.Errorf("invalid repository %q: want owner/name", s)
	}
	return s, nil
}

// resolveToken finds a GitHub token: explicit flag, GITHUB_TOKEN, GH_TOKEN,
// then the GitHub CLI's stored credentials.
func resolveToken(explicit string) (token, source string) {
	if explicit != "" {
		return explicit, "--token"
	}
	for _, env := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v, env
		}
	}
	if path, err := exec.LookPath("gh"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, path, "auth", "token").Output()
		if err == nil {
			if t := strings.TrimSpace(string(out)); t != "" {
				return t, "gh auth token"
			}
		}
	}
	return "", ""
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// progressPrinter renders a single updating status line on a terminal.
type progressPrinter struct {
	mu      sync.Mutex
	w       io.Writer
	enabled bool
	last    time.Time
	shown   bool
}

func (p *progressPrinter) update(pr collect.Progress) {
	if !p.enabled {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	final := pr.Total > 0 && pr.Done == pr.Total
	if !final && now.Sub(p.last) < 80*time.Millisecond {
		return
	}
	p.last = now
	var msg string
	switch pr.Stage {
	case "runs":
		msg = fmt.Sprintf("%s: listing workflow runs… %d", pr.Repo, pr.Done)
	case "pulls":
		msg = fmt.Sprintf("%s: indexing pull requests… %d", pr.Repo, pr.Done)
	case "jobs":
		msg = fmt.Sprintf("%s: pricing jobs… %d/%d", pr.Repo, pr.Done, pr.Total)
	}
	fmt.Fprintf(p.w, "\r\x1b[K%s", msg)
	p.shown = true
}

func (p *progressPrinter) clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.enabled && p.shown {
		fmt.Fprint(p.w, "\r\x1b[K")
		p.shown = false
	}
}

// warnf writes a warning line, clearing any progress line first.
func (p *progressPrinter) warnf(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.enabled && p.shown {
		fmt.Fprint(p.w, "\r\x1b[K")
		p.shown = false
	}
	fmt.Fprintf(p.w, "ciblame: "+format+"\n", args...)
}

// usageError marks errors that should exit with status 2.
type usageError struct {
	msg    string
	silent bool // already reported to the user
}

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ue *usageError
	if errors.As(err, &ue) || errors.Is(err, flag.ErrHelp) {
		return 2
	}
	return 1
}
