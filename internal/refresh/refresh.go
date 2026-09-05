package refresh

import (
	"bytes"
	"fmt"

	"github.com/kkamegawa/gh-repospec/internal/config"
	yamlv3 "gopkg.in/yaml.v3"
)

// Format is a configuration file's serialization.
type Format int

const (
	FormatYAML Format = iota
	FormatJSON
)

// Result is the outcome of one refresh.
type Result struct {
	// Updated is the refreshed document. It equals the original bytes
	// unchanged when Changed is false.
	Updated []byte
	Changed bool
	Plan    Plan
}

// Refresh computes what --into would change between current (the file's
// own already-loaded configuration) and desired (imported state), and
// applies it to original (the file's exact bytes, in format) without ever
// expanding what current already declares. It performs no file I/O: the
// caller decides whether and where to write Result.Updated, after its own
// diff display and confirmation.
//
// The candidate document is validated with config.ValidateDeclaredSurface
// before being returned, so a bug in this package's byte-splicing or
// JSON re-marshaling fails loudly here rather than producing a file
// config.Load would reject or apply would misinterpret. Validation
// deliberately skips seed.directory/seed.zip filesystem checks: a refresh
// candidate can be valid for the repository it describes even when the seed
// content is not present on the machine running the refresh.
func Refresh(original []byte, format Format, current, desired *config.Config) (Result, error) {
	plan := computePlan(current, desired)
	if !plan.Changed() {
		return Result{Updated: original, Changed: false, Plan: plan}, nil
	}

	var updated []byte
	var err error
	switch format {
	case FormatYAML:
		updated, err = ApplyYAML(original, plan)
	case FormatJSON:
		updated, err = ApplyJSON(current, plan)
	default:
		return Result{}, fmt.Errorf("unsupported format %v", format)
	}
	if err != nil {
		return Result{}, err
	}

	if err := validateCandidate(updated); err != nil {
		return Result{}, fmt.Errorf("refreshed document failed validation: %w", err)
	}

	return Result{Updated: updated, Changed: true, Plan: plan}, nil
}

// validateCandidate decodes data the same strict way config.Load does
// (gopkg.in/yaml.v3, which also accepts JSON as a YAML subset, with
// KnownFields so an unknown field is rejected) and validates it, skipping
// the seed filesystem checks Refresh's own doc comment explains.
func validateCandidate(data []byte) error {
	decoder := yamlv3.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var cfg config.Config
	if err := decoder.Decode(&cfg); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return config.ValidateDeclaredSurface(&cfg)
}
