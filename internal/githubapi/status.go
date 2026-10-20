package githubapi

import (
	"errors"
	"net/http"
	"strings"
)

// IsNotFound reports whether err is a GitHub 404 response. errors.As is used
// instead of a direct type assertion so a wrapped APIError (fmt.Errorf with
// %w) is still recognised.
func IsNotFound(err error) bool {
	var apiError *APIError
	return errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound
}

// IsForbidden reports whether err is a GitHub 403 response. Like IsNotFound,
// it uses errors.As so a wrapped APIError (fmt.Errorf with %w) is still
// recognised. A rate-limited request can also answer 403; callers that read
// a 403 as "refused" should rule that out with IsRateLimited.
func IsForbidden(err error) bool {
	var apiError *APIError
	return errors.As(err, &apiError) && apiError.StatusCode == http.StatusForbidden
}

// IsRefused reports whether err is a 403 that GitHub answered because it
// refuses the request itself — an unavailable feature or missing permissions
// — rather than because of a rate limit, which says nothing about the
// resource and should be treated like any other failed request.
func IsRefused(err error) bool {
	return IsForbidden(err) && !IsRateLimited(err)
}

// IsRateLimited reports whether err is a GitHub rate-limit response: a 429,
// or a 403 that carries a retry delay or GitHub's rate-limit message, which
// is how primary and secondary rate limits answer. Such a response can still
// reach a caller once the client's retries are exhausted.
func IsRateLimited(err error) bool {
	var apiError *APIError
	if !errors.As(err, &apiError) {
		return false
	}
	switch apiError.StatusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		return apiError.RetryAfter > 0 || strings.Contains(strings.ToLower(apiError.Message), "rate limit")
	default:
		return false
	}
}
