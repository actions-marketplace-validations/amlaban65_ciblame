// Package cache is a tiny on-disk JSON cache for immutable GitHub API data
// (jobs of completed runs, closed pull requests), so repeated reports only
// fetch what changed.
package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const schema = "v1"

// Cache stores JSON values under a directory. A nil *Cache is valid and
// behaves as an always-empty cache that discards writes.
type Cache struct {
	dir string
}

// DefaultDir returns the per-user cache directory for ciblame.
func DefaultDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ciblame"), nil
}

// Open returns a cache rooted at dir, creating it if needed.
func Open(dir string) (*Cache, error) {
	root := filepath.Join(dir, schema)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Cache{dir: root}, nil
}

// path maps a slash-separated key to a file inside the cache directory,
// rejecting keys that would escape it.
func (c *Cache) path(key string) (string, error) {
	if key == "" || strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return "", errors.New("cache: invalid key")
	}
	p := filepath.Join(c.dir, filepath.FromSlash(key)+".json")
	if !strings.HasPrefix(p, c.dir+string(filepath.Separator)) {
		return "", errors.New("cache: invalid key")
	}
	return p, nil
}

// Get decodes the value stored under key into v and reports whether it was found.
func (c *Cache) Get(key string, v any) bool {
	if c == nil {
		return false
	}
	p, err := c.path(key)
	if err != nil {
		return false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

// Put stores v under key. Writes are atomic (temp file + rename).
func (c *Cache) Put(key string, v any) error {
	if c == nil {
		return nil
	}
	p, err := c.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), p)
}
