package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCopilotCodeReviewSettings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		body      string
		wantError string
	}{
		{
			name: "review.yaml",
			body: "schemaVersion: 1\nrepository: {owner: octo, name: demo, visibility: public}\nsettings:\n  copilotCodeReview: {reviewEffort: inherit, mcpEnabled: false, severityFilter: low}\n",
		},
		{
			name: "review.json",
			body: `{"schemaVersion":1,"repository":{"owner":"octo","name":"demo","visibility":"public"},"settings":{"copilotCodeReview":{"reviewEffort":"inherit","mcpEnabled":false,"severityFilter":"low"}}}`,
		},
		{
			name:      "blank-effort.yaml",
			body:      "schemaVersion: 1\nrepository: {owner: octo, name: demo, visibility: public}\nsettings:\n  copilotCodeReview: {reviewEffort: ' '}\n",
			wantError: "settings.copilotCodeReview.reviewEffort",
		},
		{
			name:      "invalid-severity.json",
			body:      `{"schemaVersion":1,"repository":{"owner":"octo","name":"demo","visibility":"public"},"settings":{"copilotCodeReview":{"severityFilter":"medium"}}}`,
			wantError: "settings.copilotCodeReview.severityFilter",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), test.name)
			if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("Load() error = %v, want %s", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			review := cfg.Settings.CopilotCodeReview
			if review == nil || review.MCPEnabled == nil || *review.MCPEnabled || review.CustomInstructionsEnabled != nil ||
				review.ReviewEffort == nil || *review.ReviewEffort != "inherit" || review.SeverityFilter == nil || *review.SeverityFilter != "low" {
				t.Fatalf("decoded Copilot settings = %+v", review)
			}
		})
	}
}
