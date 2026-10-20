package githubapi

import (
	"errors"
	"fmt"
	"testing"
	"time"
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

func TestIsNotFoundRecognizesOnlyWrapped404APIError(t *testing.T) {
	t.Parallel()
	if !IsNotFound(fmt.Errorf("get AI Scan: %w", &APIError{StatusCode: 404})) {
		t.Fatal("IsNotFound did not recognize a wrapped 404 *APIError")
	}
	if IsNotFound(&APIError{StatusCode: 403}) {
		t.Fatal("IsNotFound reported a 403 as not found")
	}
	if IsNotFound(errors.New("HTTP 404")) {
		t.Fatal("IsNotFound reported a plain error as not found")
	}
}

func TestIsRateLimitedAndIsRefusedSeparateRateLimitsFromRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		err         error
		rateLimited bool
		refused     bool
	}{
		{name: "403 refusal", err: &APIError{StatusCode: 403, Message: "Advanced Security must be enabled for this repository to use code scanning."}, refused: true},
		{name: "403 with retry delay", err: fmt.Errorf("wrapped: %w", &APIError{StatusCode: 403, RetryAfter: time.Minute}), rateLimited: true},
		{name: "403 secondary rate limit message", err: &APIError{StatusCode: 403, Message: "You have exceeded a secondary rate limit."}, rateLimited: true},
		{name: "429", err: &APIError{StatusCode: 429}, rateLimited: true},
		{name: "404", err: &APIError{StatusCode: 404}},
		{name: "plain error", err: errors.New("HTTP 403")},
		{name: "nil", err: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := IsRateLimited(test.err); got != test.rateLimited {
				t.Fatalf("IsRateLimited() = %t, want %t", got, test.rateLimited)
			}
			if got := IsRefused(test.err); got != test.refused {
				t.Fatalf("IsRefused() = %t, want %t", got, test.refused)
			}
		})
	}
}
