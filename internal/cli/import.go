package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
	"github.com/kkamegawa/gh-repospec/internal/importer"
	"github.com/kkamegawa/gh-repospec/internal/refresh"
	"golang.org/x/term"
	yamlv3 "gopkg.in/yaml.v3"
)

// importOptions holds import's parsed flags. Unlike plan/apply, import
// takes a positional repository (or the -R/--repo substitute) and its own
// --format range (yaml|json, not text|json).
type importOptions struct {
	repository    string
	format        string
	formatSet     bool
	into          string
	yes           bool
	schemaVersion int
}

// importValueFlags and importBoolFlags name every flag import accepts, by
// name with no leading dashes, used to separate flags from the positional
// repository regardless of which side of it they appear on: the standard
// flag package stops parsing at the first non-flag argument, which would
// otherwise make "import OWNER/REPO --format json" treat --format as a
// second positional argument.
var (
	importValueFlags = map[string]bool{"repo": true, "R": true, "format": true, "into": true, "schema-version": true}
	importBoolFlags  = map[string]bool{"yes": true}
)

func parseImportFlags(args []string, stderr io.Writer) (importOptions, error) {
	flagArgs, positional, err := splitFlagsAndPositional(args, importValueFlags, importBoolFlags)
	if err != nil {
		return importOptions{}, err
	}

	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options importOptions
	var repoFlag string
	flags.StringVar(&repoFlag, "repo", "", "target repository in owner/name form")
	flags.StringVar(&repoFlag, "R", "", "target repository in owner/name form")
	flags.StringVar(&options.format, "format", "yaml", "output format: yaml or json")
	flags.StringVar(&options.into, "into", "", "refresh an existing configuration file in place instead of writing a new document")
	flags.IntVar(&options.schemaVersion, "schema-version", config.CurrentSchemaVersion, "schema version for a new repository export (1 or 2)")
	flags.BoolVar(&options.yes, "yes", false, "skip the --into confirmation prompt")
	if err := flags.Parse(flagArgs); err != nil {
		return importOptions{}, err
	}

	switch {
	case len(positional) > 1:
		return importOptions{}, fmt.Errorf("import accepts exactly one repository, got %d", len(positional))
	case len(positional) == 1 && repoFlag != "":
		return importOptions{}, errors.New("specify the repository as a positional argument or with -R/--repo, not both")
	case len(positional) == 1:
		options.repository = positional[0]
	case repoFlag != "":
		options.repository = repoFlag
	default:
		return importOptions{}, errors.New("a repository is required: gh repospec import <owner/repository> or -R <owner/repository>")
	}

	if options.format != "yaml" && options.format != "json" {
		return importOptions{}, fmt.Errorf("--format must be yaml or json, got %q", options.format)
	}
	if options.yes && options.into == "" {
		return importOptions{}, errors.New("--yes has no effect without --into")
	}
	if options.schemaVersion != config.CurrentSchemaVersion && options.schemaVersion != config.SchemaVersion2 {
		return importOptions{}, errors.New("--schema-version must be 1 or 2")
	}
	var versionSet bool
	flags.Visit(func(f *flag.Flag) {
		options.formatSet = options.formatSet || f.Name == "format"
		versionSet = versionSet || f.Name == "schema-version"
	})
	if versionSet && options.into != "" {
		return importOptions{}, errors.New("--schema-version cannot be used with --into; the destination determines the version")
	}
	return options, nil
}

// splitFlagsAndPositional separates known flags (and, for a value flag, the
// argument right after it) from everything else, so the caller's flag.FlagSet
// sees every flag regardless of whether it appeared before or after the
// positional argument. An argument starting with "-" that is not a known
// flag is rejected outright rather than silently treated as positional.
//
// valueFlags and boolFlags are keyed by bare flag name (no leading dashes),
// since Go's flag package itself treats "-x" and "--x" as equivalent for any
// flag; matching on the bare name here accepts both spellings the same way
// flag.Parse would. "-h", "-help", and "--help" are always recognized and
// passed through, matching flag.FlagSet's built-in help handling.
func splitFlagsAndPositional(args []string, valueFlags, boolFlags map[string]bool) (flagArgs, positional []string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		trimmed := strings.TrimLeft(arg, "-")
		if trimmed == arg || trimmed == "" {
			// No leading dash (or the bare "-" stdin marker): positional.
			positional = append(positional, arg)
			continue
		}
		name, _, hasEquals := strings.Cut(trimmed, "=")
		switch {
		case name == "h" || name == "help":
			flagArgs = append(flagArgs, arg)
		case boolFlags[name]:
			flagArgs = append(flagArgs, arg)
		case valueFlags[name] && hasEquals:
			flagArgs = append(flagArgs, arg)
		case valueFlags[name]:
			flagArgs = append(flagArgs, arg)
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("flag %s requires a value", arg)
			}
			i++
			flagArgs = append(flagArgs, args[i])
		default:
			return nil, nil, fmt.Errorf("unknown flag %q", arg)
		}
	}
	return flagArgs, positional, nil
}

func parseOwnerRepo(value string) (githubapi.Repository, error) {
	owner, name, ok := strings.Cut(value, "/")
	if !ok || owner == "" || name == "" || strings.Contains(owner, " ") || strings.Contains(name, " ") || strings.Contains(name, "/") {
		return githubapi.Repository{}, fmt.Errorf("repository must be owner/name, got %q", value)
	}
	return githubapi.Repository{Owner: owner, Name: name}, nil
}

func executeImport(ctx context.Context, options importOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	repository, err := parseOwnerRepo(options.repository)
	if err != nil {
		return err
	}
	var destination *repositoryRefresh
	if options.into != "" {
		destination, err = readRepositoryRefresh(options)
		if err != nil {
			return err
		}
	}
	client, err := githubapi.NewAuthenticatedClient()
	if err != nil {
		return err
	}
	result, err := importer.Import(ctx, client, repository)
	if err != nil {
		return err
	}
	for _, diagnostic := range result.Diagnostics {
		if _, err := fmt.Fprintln(stderr, diagnostic.String()); err != nil {
			return err
		}
	}

	if destination == nil {
		if options.schemaVersion == config.SchemaVersion2 {
			result.Config.SchemaVersion = config.SchemaVersion2
			result.Config.Scope = config.ScopeRepository
		}
		return writeImportDocument(stdout, result.Config, options.format)
	}
	return destination.refresh(result, options, stdin, stdout, stderr)
}

func writeImportDocument(output io.Writer, cfg any, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(cfg)
	}
	encoder := yamlv3.NewEncoder(output)
	encoder.SetIndent(2)
	if err := encoder.Encode(cfg); err != nil {
		return err
	}
	return encoder.Close()
}

type repositoryRefresh struct {
	original []byte
	format   refresh.Format
	current  *config.Config
}

func readRepositoryRefresh(options importOptions) (*repositoryRefresh, error) {
	original, err := os.ReadFile(options.into)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", options.into, err)
	}
	format, err := resolveIntoFormat(options, original)
	if err != nil {
		return nil, err
	}
	current, err := config.DecodeRepository(original)
	if err != nil {
		return nil, fmt.Errorf("decode configuration %q: %w", options.into, err)
	}
	if err := config.ValidateDeclaredSurface(current); err != nil {
		return nil, fmt.Errorf("validate configuration %q: %w", options.into, err)
	}
	return &repositoryRefresh{original: original, format: format, current: current}, nil
}

func (r *repositoryRefresh) refresh(result importer.Result, options importOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	outcome, err := refresh.Refresh(r.original, r.format, r.current, &result.Config)
	if err != nil {
		return err
	}
	if !outcome.Changed {
		_, err := fmt.Fprintln(stdout, "no changes")
		return err
	}

	if err := printRefreshPlan(stderr, outcome.Plan); err != nil {
		return err
	}
	return saveRefresh(options.into, r.original, outcome.Updated, options.yes, stdin, stdout, stderr)
}

func saveRefresh(path string, original, updated []byte, yes bool, stdin io.Reader, stdout, stderr io.Writer) error {
	if !yes {
		if !isTerminal(stdin) {
			return errors.New("refusing to prompt on non-interactive input; pass --yes to confirm --into without a prompt")
		}
		confirmed, err := confirm(stdin, stderr, fmt.Sprintf("Write the refreshed configuration to %s?", path))
		if err != nil {
			return err
		}
		if !confirmed {
			return errors.New("refresh cancelled")
		}
	}

	current, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("re-read %s: %w", path, err)
	}
	if !bytes.Equal(current, original) {
		return fmt.Errorf("%s changed during refresh; retry with the current file", path)
	}
	if err := replaceRefreshFile(path, updated); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	_, err = fmt.Fprintf(stdout, "refreshed %s\n", path)
	return err
}

func replaceRefreshFile(path string, updated []byte) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".repospec-refresh-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(updated); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(info.Mode().Perm()); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), target)
}

// resolveIntoFormat detects the existing file's actual format and rejects an
// explicit --format that disagrees with it. yaml.v3 (used here exactly as
// config.Load uses it) accepts JSON as a YAML subset, so detection tries a
// strict JSON decode first and falls back to YAML.
func resolveIntoFormat(options importOptions, original []byte) (refresh.Format, error) {
	detected := refresh.FormatYAML
	if json.Valid(original) {
		detected = refresh.FormatJSON
	}
	explicit := explicitFormat(options)
	if explicit == "" {
		return detected, nil
	}
	if (explicit == "json") != (detected == refresh.FormatJSON) {
		return 0, fmt.Errorf("--format %s does not match the format of %s", explicit, options.into)
	}
	return detected, nil
}

// explicitFormat reports the --format value when the caller supplied it.
func explicitFormat(options importOptions) string {
	if options.formatSet || options.format == "json" {
		return options.format
	}
	return ""
}

// printRefreshPlan reports every change --into is about to make, so the
// confirmation prompt (or --yes in a script's own log) shows exactly what
// changed, including which declared entries were left alone because GitHub
// does not have them.
func printRefreshPlan(output io.Writer, plan refresh.Plan) error {
	for _, sc := range plan.Scalars {
		if _, err := fmt.Fprintf(output, "UPDATE %s\n", sc.Path); err != nil {
			return err
		}
	}
	for _, cc := range plan.Collections {
		for _, upd := range cc.Updated {
			if _, err := fmt.Fprintf(output, "UPDATE %s[%s]\n", cc.Path, upd.Key); err != nil {
				return err
			}
		}
		for _, add := range cc.Added {
			if _, err := fmt.Fprintf(output, "ADD %s[%s]\n", cc.Path, add.Key); err != nil {
				return err
			}
		}
		for _, key := range cc.NotApplied {
			if _, err := fmt.Fprintf(output, "NOT APPLIED %s[%s]: declared but not found on GitHub\n", cc.Path, key); err != nil {
				return err
			}
		}
	}
	if plan.Rulesets != nil {
		for _, upd := range plan.Rulesets.Updated {
			if _, err := fmt.Fprintf(output, "UPDATE rulesets[%s]\n", upd.Name); err != nil {
				return err
			}
		}
		for _, add := range plan.Rulesets.Added {
			if _, err := fmt.Fprintf(output, "ADD rulesets[%s]\n", add.Name); err != nil {
				return err
			}
		}
		for _, key := range plan.Rulesets.NotApplied {
			if _, err := fmt.Fprintf(output, "NOT APPLIED rulesets[%s]: declared but not found on GitHub\n", key); err != nil {
				return err
			}
		}
	}
	return nil
}

// isTerminal reports whether r is connected to an interactive terminal. It
// only ever recognizes *os.File (production passes os.Stdin; tests pass an
// in-memory reader and correctly see a non-terminal).
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
