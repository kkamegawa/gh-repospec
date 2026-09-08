// Package cli implements the command-line interface for gh-repospec.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
	"github.com/kkamegawa/gh-repospec/internal/planner"
)

// Version is the released version. It defaults to "dev" for local builds and is set
// at build time with -ldflags "-X .../internal/cli.Version=vX.Y.Z" from the release
// git tag, matching gh CLI extension conventions.
var Version = "dev"

// DriftError reports that plan --ci found at least one convergeable
// difference. It is a successful detection, not a failure: the process
// exits 2 and the caller must not print it with an "error:" prefix. plan
// writes the human-readable notice to standard error itself, so the report
// on standard output stays a clean, valid document in both formats.
type DriftError struct{ Changes int }

func (e *DriftError) Error() string { return fmt.Sprintf("plan --ci found %d change(s)", e.Changes) }

// ExitCode reports the process exit code a DriftError maps to.
func (e *DriftError) ExitCode() int { return 2 }

// Run executes a gh-repospec command using the supplied standard streams.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("a command is required")
	}

	switch args[0] {
	case "version", "--version", "-v":
		_, err := fmt.Fprintf(stdout, "gh-repospec %s\n", Version)
		return err
	case "validate":
		file, err := parseValidateFlags(args[1:], stderr)
		if err != nil {
			return err
		}
		if _, err := config.Load(file); err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, "configuration is valid")
		return err
	case "plan":
		options, err := parseExecutionFlags("plan", args[1:], stderr)
		if err != nil {
			return err
		}
		return executePlan(context.Background(), options, stdout, stderr)
	case "apply":
		options, err := parseExecutionFlags("apply", args[1:], stderr)
		if err != nil {
			return err
		}
		return executeApply(context.Background(), options, stdin, stdout, stderr)
	case "import":
		options, err := parseImportFlags(args[1:], stderr)
		if err != nil {
			return err
		}
		return executeImport(context.Background(), options, stdin, stdout, stderr)
	case "help", "--help", "-h":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type executionOptions struct {
	file             string
	repository       string
	format           string
	ci               bool
	yes              bool
	approvedCommands stringSet
}

func parseValidateFlags(args []string, stderr io.Writer) (string, error) {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "path to a YAML or JSON configuration file")
	flags.StringVar(file, "f", "", "path to a YAML or JSON configuration file")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if flags.NArg() != 0 {
		return "", errors.New("validate accepts no positional arguments")
	}
	if *file == "" {
		return "", errors.New("--file is required")
	}
	return *file, nil
}

func parseExecutionFlags(command string, args []string, stderr io.Writer) (executionOptions, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options executionOptions
	flags.StringVar(&options.file, "file", "", "path to a YAML or JSON configuration file")
	flags.StringVar(&options.file, "f", "", "path to a YAML or JSON configuration file")
	flags.StringVar(&options.repository, "repo", "", "target repository in owner/name form")
	flags.StringVar(&options.repository, "R", "", "target repository in owner/name form")
	flags.StringVar(&options.format, "format", "text", "output format: text or json")
	if command == "plan" {
		flags.BoolVar(&options.ci, "ci", false, "exit 2 when the plan contains a difference, 1 when it is blocked")
	}
	if command == "apply" {
		flags.BoolVar(&options.yes, "yes", false, "apply without the remote-change confirmation prompt")
		flags.Var(&options.approvedCommands, "approve-command", "explicitly approve one configured command ID (repeatable)")
	}
	if err := flags.Parse(args); err != nil {
		return executionOptions{}, err
	}
	if flags.NArg() != 0 {
		return executionOptions{}, fmt.Errorf("%s accepts no positional arguments", command)
	}
	if options.file == "" {
		return executionOptions{}, errors.New("--file is required")
	}
	if options.format != "text" && options.format != "json" {
		return executionOptions{}, fmt.Errorf("--format must be text or json, got %q", options.format)
	}
	return options, nil
}

func executePlan(ctx context.Context, options executionOptions, stdout, stderr io.Writer) error {
	cfg, engine, err := loadEngine(options.file)
	if err != nil {
		return err
	}
	plan, err := engine.Build(ctx, cfg, planner.Options{RepositoryOverride: options.repository})
	if err != nil {
		return err
	}
	if err := writePlan(stdout, plan, options.format); err != nil {
		return err
	}
	return planOutcome(stderr, plan, options.ci)
}

// planOutcome maps a finished plan onto the exit-code contract. Order
// matters: a blocked entry is a failure, so it outranks drift. plan without
// --ci always returns nil, so its exit code is unchanged by this feature.
func planOutcome(stderr io.Writer, plan planner.Plan, ci bool) error {
	if !ci {
		return nil
	}
	if plan.HasBlocked() {
		return errors.New("plan is blocked; resolve blocked plan entries before retrying")
	}
	if count := plan.DriftCount(); count > 0 {
		if _, err := fmt.Fprintf(stderr, "drift detected: %d change(s)\n", count); err != nil {
			return err
		}
		return &DriftError{Changes: count}
	}
	return nil
}

func executeApply(ctx context.Context, options executionOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	cfg, engine, err := loadEngine(options.file)
	if err != nil {
		return err
	}
	for _, command := range cfg.Commands {
		if !options.approvedCommands[command.ID] {
			return fmt.Errorf("command %q requires --approve-command %s", command.ID, command.ID)
		}
	}
	plan, err := engine.Build(ctx, cfg, planner.Options{RepositoryOverride: options.repository})
	if err != nil {
		return err
	}
	if err := writePlan(stdout, plan, options.format); err != nil {
		return err
	}
	if plan.HasBlocked() {
		return errors.New("apply is blocked; resolve blocked plan entries before retrying")
	}
	if !options.yes {
		confirmed, err := confirm(stdin, stderr, fmt.Sprintf("Apply the planned remote changes to %s?", plan.Repository))
		if err != nil {
			return err
		}
		if !confirmed {
			return errors.New("apply cancelled")
		}
	}
	_, err = engine.Apply(ctx, cfg, planner.Options{
		RepositoryOverride: options.repository,
		ApprovedCommandIDs: options.approvedCommands,
	})
	return err
}

func loadEngine(path string) (*config.Config, *planner.Engine, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, err
	}
	client, err := githubapi.NewAuthenticatedClient()
	if err != nil {
		return nil, nil, err
	}
	engine, err := planner.New(client)
	if err != nil {
		return nil, nil, err
	}
	return cfg, engine, nil
}

func writePlan(output io.Writer, plan planner.Plan, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(plan)
	}
	if _, err := fmt.Fprintf(output, "Repository: %s\n", plan.Repository); err != nil {
		return err
	}
	if plan.Exists {
		if _, err := fmt.Fprintln(output, "Repository exists."); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(output, "Repository will be created."); err != nil {
		return err
	}
	if len(plan.Changes) == 0 {
		_, err := fmt.Fprintln(output, "No changes.")
		return err
	}
	for _, change := range plan.Changes {
		if _, err := fmt.Fprintf(output, "[%s] %s %s: %s\n", change.Stage, strings.ToUpper(string(change.State)), change.Resource, change.Summary); err != nil {
			return err
		}
	}
	return nil
}

func confirm(input io.Reader, output io.Writer, prompt string) (bool, error) {
	if _, err := fmt.Fprintf(output, "%s [y/N] ", prompt); err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(input)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, fmt.Errorf("read confirmation: %w", err)
		}
		return false, nil
	}
	response := strings.TrimSpace(scanner.Text())
	return strings.EqualFold(response, "y") || strings.EqualFold(response, "yes"), nil
}

type stringSet map[string]bool

func (values *stringSet) String() string {
	if values == nil || len(*values) == 0 {
		return ""
	}
	items := make([]string, 0, len(*values))
	for value := range *values {
		items = append(items, value)
	}
	return strings.Join(items, ",")
}

func (values *stringSet) Set(value string) error {
	if value == "" {
		return errors.New("approved command ID cannot be empty")
	}
	if *values == nil {
		*values = make(stringSet)
	}
	(*values)[value] = true
	return nil
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage: gh repospec <command> [flags]

Commands:
  validate --file <path>                          Validate a schemaVersion 1 YAML or JSON configuration.
  plan --file <path> [-R owner/repository] [--ci] Preview changes without modifying GitHub.
  apply --file <path> [-R owner/repository]       Confirm and apply supported changes.
  import <owner/repository> [--into <path>]       Read a repository and write or refresh a configuration.
  version, --version, -v                          Print the gh-repospec version.

Common flags:
  -f, --file <path>        Configuration path (required for validate/plan/apply).
  -R, --repo <owner/name>  Override the repository declared in the configuration, or substitute for
                           import's positional repository (not both).
  --format <text|json>     Select plan output format (import instead uses --format <yaml|json>).

Plan flags:
  --ci                     Exit 2 when the plan contains a difference, 1 when it is blocked. Secret
                           updates never count as a difference, because GitHub never returns their
                           value. Without --ci, plan always exits 0.

Apply flags:
  --yes                    Skip the remote-change confirmation prompt.
  --approve-command <id>   Approve a configured command; repeat for each command.

Import flags:
  --format <yaml|json>     Select the written document's format (default yaml).
  --into <path>            Refresh an existing configuration file in place instead of writing a new
                           document. Only paths and sections the file already declares are refreshed;
                           a declared entry GitHub does not have is kept and reported, never deleted.
  --yes                    Skip the --into confirmation prompt.

import never writes to GitHub. It reports secret names as diagnostics on
standard error; secret values are never returned by the GitHub API.
`)
}
