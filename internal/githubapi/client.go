package githubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	ghapi "github.com/cli/go-gh/v2/pkg/api"
)

const githubAPIURL = "https://api.github.com/"

// Doer makes the client straightforward to test with httptest and planner fakes.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// RetryPolicy controls retries for idempotent requests only.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
}

// Client is a token-safe GitHub REST API client.
type Client struct {
	baseURL     *url.URL
	httpClient  Doer
	retryPolicy RetryPolicy
}

// NewAuthenticatedClient creates a GitHub.com client using the existing gh login.
// go-gh obtains authentication from the GitHub CLI environment and config; this package
// never obtains, stores, or logs the access token.
func NewAuthenticatedClient() (*Client, error) {
	if host := os.Getenv("GH_HOST"); host != "" && host != "github.com" {
		return nil, fmt.Errorf("unsupported GitHub host %q: gh-repobootstrapper supports github.com only", host)
	}
	httpClient, err := ghapi.DefaultHTTPClient()
	if err != nil {
		return nil, fmt.Errorf("initialize GitHub CLI authentication: %w", err)
	}
	return NewClient(httpClient, githubAPIURL)
}

// NewClient creates a client for an explicit API URL. It is primarily useful for tests.
func NewClient(httpClient Doer, baseURL string) (*Client, error) {
	if httpClient == nil {
		return nil, errors.New("GitHub HTTP client is required")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid GitHub API URL %q", baseURL)
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return &Client{
		baseURL:     u,
		httpClient:  httpClient,
		retryPolicy: RetryPolicy{MaxAttempts: 3, BaseDelay: 200 * time.Millisecond},
	}, nil
}

// SetRetryPolicy changes retry behavior. It is intended for tests and controlled callers.
func (c *Client) SetRetryPolicy(policy RetryPolicy) {
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = 1
	}
	if policy.BaseDelay <= 0 {
		policy.BaseDelay = 200 * time.Millisecond
	}
	c.retryPolicy = policy
}

func (c *Client) do(ctx context.Context, method, apiPath string, input, output any) error {
	if !strings.HasPrefix(apiPath, "/") {
		return fmt.Errorf("GitHub API path must start with '/': %q", apiPath)
	}
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode GitHub API request: %w", err)
		}
	}

	attempts := c.retryPolicy.MaxAttempts
	for attempt := 1; attempt <= attempts; attempt++ {
		requestURL, err := c.resolveAPIPath(apiPath)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create GitHub API request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", APIVersion)
		if input != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt < attempts && retryableMethod(method) && ctx.Err() == nil {
				if waitErr := waitForRetry(ctx, retryDelay(c.retryPolicy, attempt, 0)); waitErr != nil {
					return waitErr
				}
				continue
			}
			return fmt.Errorf("perform GitHub API request: %w", err)
		}

		apiErr := responseError(method, apiPath, resp)
		if apiErr != nil {
			if attempt < attempts && retryableMethod(method) && shouldRetry(apiErr) {
				if waitErr := waitForRetry(ctx, retryDelay(c.retryPolicy, attempt, apiErr.RetryAfter)); waitErr != nil {
					return waitErr
				}
				continue
			}
			return apiErr
		}
		defer resp.Body.Close()
		if output != nil && resp.StatusCode != http.StatusNoContent {
			if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(output); err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("decode GitHub API response: %w", err)
			}
		}
		return nil
	}
	return errors.New("GitHub API retry loop exhausted")
}

func (c *Client) resolveAPIPath(apiPath string) (*url.URL, error) {
	relative, err := url.Parse(strings.TrimPrefix(apiPath, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse GitHub API path %q: %w", apiPath, err)
	}
	if relative.IsAbs() || relative.Host != "" {
		return nil, fmt.Errorf("GitHub API path must not include a host: %q", apiPath)
	}
	return c.baseURL.ResolveReference(relative), nil
}

func responseError(method, path string, resp *http.Response) *APIError {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	defer resp.Body.Close()
	var payload struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload)
	return &APIError{
		StatusCode: resp.StatusCode,
		Method:     method,
		Path:       path,
		Message:    payload.Message,
		RequestID:  resp.Header.Get("X-GitHub-Request-Id"),
		RetryAfter: retryAfter(resp.Header),
	}
}

func retryAfter(header http.Header) time.Duration {
	if seconds, err := strconv.Atoi(header.Get("Retry-After")); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if header.Get("X-RateLimit-Remaining") == "0" {
		if reset, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			if delay := time.Until(time.Unix(reset, 0)); delay > 0 {
				return delay
			}
		}
	}
	return 0
}

func retryableMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func shouldRetry(err *APIError) bool {
	return err.StatusCode == http.StatusTooManyRequests || err.StatusCode >= 500 || (err.StatusCode == http.StatusForbidden && err.RetryAfter > 0)
}

func retryDelay(policy RetryPolicy, attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	return policy.BaseDelay * time.Duration(1<<(attempt-1))
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func itoa(value int) string { return strconv.Itoa(value) }
