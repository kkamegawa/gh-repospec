package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateFileScopes(t *testing.T) {
	t.Parallel()
	repo := "repository: {owner: octocat, name: example, visibility: private}\n"
	personal := "schemaVersion: 2\nscope: personal\naccount: octocat\n"
	cases := []struct {
		name string
		data string
		ok   bool
	}{
		{"v1", "schemaVersion: 1\n" + repo, true},
		{"v2 repository", "schemaVersion: 2\nscope: repository\n" + repo, true},
		{"v2 personal", personal + "issueTypes: [{name: Bug, isEnabled: false}]\n", true},
		{"personal JSON", `{"schemaVersion":2,"scope":"personal","account":"octocat","issueFields":[{"name":"Target date","dataType":"date"}]}`, true},
		{"v1 scope", "schemaVersion: 1\nscope: null\n" + repo, false},
		{"missing scope", "schemaVersion: 2\n" + repo, false},
		{"mixed personal", personal + repo, false},
		{"mixed repository", "schemaVersion: 2\nscope: repository\nissueFields: []\n" + repo, false},
		{"duplicate names", personal + "issueTypes: [{name: Bug, isEnabled: true}, {name: bug, isEnabled: false}]\n", false},
		{"wrong enabled type", personal + "issueTypes: [{name: Bug, isEnabled: 'true'}]\n", false},
		{"options on date", personal + "issueFields: [{name: Date, dataType: date, options: []}]\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := ValidateFile(path); (err == nil) != tc.ok {
				t.Fatalf("ValidateFile() = %v, want success %v", err, tc.ok)
			}
		})
	}
}
