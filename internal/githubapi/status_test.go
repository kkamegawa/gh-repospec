package githubapi

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsForbiddenRecognizesOnlyWrapped403APIError(t *testing.T) {
	t.Parallel()
	if !IsForbidden(fmt.Errorf("get AI Scan: %w", &APIError{StatusCode: 403})) {
		t.Fatal("IsForbidden did not recognize a wrapped 403 *APIError")
	}
	if IsForbidden(&APIError{StatusCode: 404}) {
		t.Fatal("IsForbidden reported a 404 as forbidden")
	}
	if IsForbidden(errors.New("HTTP 403")) {
		t.Fatal("IsForbidden reported a plain error as forbidden")
	}
	if IsForbidden(nil) {
		t.Fatal("IsForbidden reported nil as forbidden")
	}
}
