package githubapi

import (
	"errors"
	"net/http"
)

// IsForbidden reports whether err is a GitHub 403 response. Like IsNotFound,
// it uses errors.As so a wrapped APIError (fmt.Errorf with %w) is still
// recognised.
func IsForbidden(err error) bool {
	var apiError *APIError
	return errors.As(err, &apiError) && apiError.StatusCode == http.StatusForbidden
}
