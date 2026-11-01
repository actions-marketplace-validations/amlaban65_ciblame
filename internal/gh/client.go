package gh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultBaseURL is the public GitHub REST API endpoint.
const DefaultBaseURL = "https://api.github.com"

var (
	// ErrNotFound is returned for 404 responses.
	ErrNotFound = errors.New("not found")
	// ErrUnauthorized is returned for 401 responses.
	ErrUnauthorized = errors.New("unauthorized")
)

// RateLimitError is returned when the primary rate limit is exhausted and
// waiting for the reset would take longer than Client.MaxRateLimitWait.
type RateLimitError struct {
	Reset time.Time
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("GitHub API rate limit exhausted until %s (authenticate with GITHUB_TOKEN for 5,000 requests/hour)",
		e.Reset.Local().Format("15:04 MST"))
}

// APIError is a non-success response that is not otherwise classified.
type APIError struct {
	StatusCode int
	Message    string
	URL        string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("GitHub API %d for %s: %s", e.StatusCode, e.URL, e.Message)
}

// Client talks to the GitHub REST API. The zero value is not usable; use NewClient.
type Client struct {
	BaseURL   string
	Token     string
	HTTP      *http.Client
	UserAgent string

	// MaxRetries bounds retries for transient failures (network errors, 5xx).
	MaxRetries int
	// MaxRateLimitWait is the longest the client will sleep for a rate limit
	// reset before giving up with a RateLimitError.
	MaxRateLimitWait time.Duration
	// Logf receives human-readable notices (rate-limit waits, truncation warnings).
	Logf func(format string, args ...any)

	// sleep and now are replaceable in tests.
	sleep func(ctx context.Context, d time.Duration) error
	now   func() time.Time

	requests atomic.Int64
}

// NewClient returns a client for api.github.com authenticated with token
// (which may be empty for anonymous, heavily rate-limited access).
func NewClient(token string) *Client {
	return &Client{
		BaseURL:          DefaultBaseURL,
		Token:            token,
		HTTP:             &http.Client{Timeout: 60 * time.Second},
		UserAgent:        "ciblame",
		MaxRetries:       3,
		MaxRateLimitWait: 15 * time.Minute,
	}
}

// Requests reports how many HTTP requests the client has issued.
func (c *Client) Requests() int64 { return c.requests.Load() }

func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

func (c *Client) doSleep(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// endpoint builds an absolute URL for an API path and query.
func (c *Client) endpoint(path string, q url.Values) string {
	base := strings.TrimRight(c.BaseURL, "/")
	u := base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// getJSON GETs rawURL, decodes the body into out and returns the URL of the
// next page (from the Link header), if any.
func (c *Client) getJSON(ctx context.Context, rawURL string, out any) (next string, err error) {
	retries, rateWaits := 0, 0
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", c.UserAgent)
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}

		c.requests.Add(1)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if retries < c.MaxRetries {
				retries++
				if err := c.doSleep(ctx, backoff(retries)); err != nil {
					return "", err
				}
				continue
			}
			return "", fmt.Errorf("GET %s: %w", rawURL, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusOK:
			if readErr != nil {
				return "", fmt.Errorf("reading %s: %w", rawURL, readErr)
			}
			if out != nil {
				if err := json.Unmarshal(body, out); err != nil {
					return "", fmt.Errorf("decoding %s: %w", rawURL, err)
				}
			}
			return nextLink(resp.Header.Get("Link")), nil

		case resp.StatusCode == http.StatusUnauthorized:
			return "", fmt.Errorf("%w: %s (check that your token is valid)", ErrUnauthorized, apiMessage(body))

		case resp.StatusCode == http.StatusNotFound:
			return "", fmt.Errorf("%w: %s", ErrNotFound, rawURL)

		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
			wait, limited, reset := c.rateLimitWait(resp, body)
			if !limited {
				return "", &APIError{StatusCode: resp.StatusCode, Message: apiMessage(body), URL: rawURL}
			}
			if wait > c.MaxRateLimitWait || rateWaits >= 5 {
				return "", &RateLimitError{Reset: reset}
			}
			rateWaits++
			c.logf("GitHub rate limit reached; waiting %s before retrying", wait.Round(time.Second))
			if err := c.doSleep(ctx, wait); err != nil {
				return "", err
			}

		case resp.StatusCode >= 500:
			if retries < c.MaxRetries {
				retries++
				if err := c.doSleep(ctx, backoff(retries)); err != nil {
					return "", err
				}
				continue
			}
			return "", &APIError{StatusCode: resp.StatusCode, Message: apiMessage(body), URL: rawURL}

		default:
			return "", &APIError{StatusCode: resp.StatusCode, Message: apiMessage(body), URL: rawURL}
		}
	}
}

// rateLimitWait decides whether a 403/429 is a rate limit and, if so, how long to wait.
func (c *Client) rateLimitWait(resp *http.Response, body []byte) (wait time.Duration, limited bool, reset time.Time) {
	now := c.clock()
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(ra)); err == nil {
			d := time.Duration(secs) * time.Second
			return d, true, now.Add(d)
		}
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		if r, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			reset = time.Unix(r, 0)
			d := reset.Sub(now) + time.Second
			if d < time.Second {
				d = time.Second
			}
			return d, true, reset
		}
		return time.Minute, true, now.Add(time.Minute)
	}
	if strings.Contains(strings.ToLower(apiMessage(body)), "rate limit") {
		return time.Minute, true, now.Add(time.Minute)
	}
	return 0, false, time.Time{}
}

func backoff(attempt int) time.Duration {
	d := time.Duration(math.Pow(2, float64(attempt-1))) * 500 * time.Millisecond
	if d > 10*time.Second {
		d = 10 * time.Second
	}
	return d
}

func apiMessage(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return e.Message
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	if s == "" {
		return "(empty response)"
	}
	return s
}

// nextLink extracts the rel="next" URL from an RFC 8288 Link header.
func nextLink(h string) string {
	for _, part := range strings.Split(h, ",") {
		segs := strings.Split(part, ";")
		if len(segs) < 2 {
			continue
		}
		isNext := false
		for _, s := range segs[1:] {
			if strings.TrimSpace(s) == `rel="next"` {
				isNext = true
			}
		}
		if !isNext {
			continue
		}
		u := strings.TrimSpace(segs[0])
		return strings.TrimSuffix(strings.TrimPrefix(u, "<"), ">")
	}
	return ""
}
