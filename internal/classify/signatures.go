package classify

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

//go:embed signatures.json
var builtinSignatures []byte

// Signature describes how to recognise one AI agent or automation bot.
// Every list is optional; a signature matches when any of its signals match.
type Signature struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   Kind   `json:"kind"` // "agent" or "bot"
	Vendor string `json:"vendor,omitempty"`
	Notes  string `json:"notes,omitempty"`

	// Logins are GitHub account logins (case-insensitive) of the agent's
	// identity, matched against the pull request author and the run actor.
	Logins []string `json:"logins,omitempty"`
	// WorkflowPaths are case-insensitive substrings of the workflow path.
	// A match means the run is the agent itself executing on Actions.
	WorkflowPaths []string `json:"workflow_paths,omitempty"`
	// WorkflowPathSuffixes are matched against the end of the workflow path.
	WorkflowPathSuffixes []string `json:"workflow_path_suffixes,omitempty"`
	// BranchPrefixes are matched against the pull request head branch (or
	// the run's branch when there is no pull request).
	BranchPrefixes []string `json:"branch_prefixes,omitempty"`
	// Trailers are regular expressions (case-insensitive, multi-line) matched
	// against the head commit message, e.g. Co-authored-by trailers.
	Trailers []string `json:"trailers,omitempty"`
	// Emails are matched against the head commit author email and against
	// addresses in Co-authored-by trailers.
	Emails []string `json:"emails,omitempty"`
	// BodyPatterns are regular expressions matched against the pull request body.
	BodyPatterns []string `json:"body_patterns,omitempty"`

	trailers []*regexp.Regexp
	bodies   []*regexp.Regexp
}

type signatureFile struct {
	Version    int         `json:"version"`
	Signatures []Signature `json:"signatures"`
}

// Builtin returns the signatures shipped with ciblame.
func Builtin() ([]Signature, error) {
	return Parse(builtinSignatures)
}

// LoadFile reads extra signatures from a JSON file in the same format as the
// built-in signatures.json.
func LoadFile(path string) ([]Signature, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sigs, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return sigs, nil
}

// Parse decodes, validates and compiles a signature file.
func Parse(data []byte) ([]Signature, error) {
	var f signatureFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing signatures: %w", err)
	}
	seen := map[string]bool{}
	for i := range f.Signatures {
		s := &f.Signatures[i]
		if s.ID == "" || s.Name == "" {
			return nil, fmt.Errorf("signature #%d: id and name are required", i+1)
		}
		if seen[s.ID] {
			return nil, fmt.Errorf("signature %q: duplicate id", s.ID)
		}
		seen[s.ID] = true
		if s.Kind != KindAgent && s.Kind != KindBot {
			return nil, fmt.Errorf("signature %q: kind must be %q or %q", s.ID, KindAgent, KindBot)
		}
		for _, p := range s.Trailers {
			re, err := regexp.Compile("(?im)" + p)
			if err != nil {
				return nil, fmt.Errorf("signature %q: trailer pattern %q: %w", s.ID, p, err)
			}
			s.trailers = append(s.trailers, re)
		}
		for _, p := range s.BodyPatterns {
			re, err := regexp.Compile("(?i)" + p)
			if err != nil {
				return nil, fmt.Errorf("signature %q: body pattern %q: %w", s.ID, p, err)
			}
			s.bodies = append(s.bodies, re)
		}
	}
	return f.Signatures, nil
}

func (s *Signature) hasLogin(login string) bool {
	for _, l := range s.Logins {
		if strings.EqualFold(l, login) {
			return true
		}
	}
	return false
}

// Signals returns a compact description of what the signature matches on.
func (s *Signature) Signals() []string {
	var out []string
	add := func(label string, n int) {
		if n > 0 {
			out = append(out, label)
		}
	}
	add("login", len(s.Logins))
	add("workflow", len(s.WorkflowPaths)+len(s.WorkflowPathSuffixes))
	add("trailer", len(s.Trailers))
	add("email", len(s.Emails))
	add("pr-body", len(s.BodyPatterns))
	add("branch", len(s.BranchPrefixes))
	return out
}
