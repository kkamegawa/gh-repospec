package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
	"github.com/kkamegawa/gh-repospec/internal/personal"
	"github.com/kkamegawa/gh-repospec/internal/refresh"
)

type personalPlanOptions struct {
	file   string
	format string
	ci     bool
}

type personalApplyOptions struct {
	file   string
	format string
	yes    bool
}

type personalImportOptions struct {
	format    string
	formatSet bool
	into      string
	yes       bool
}

// runPersonal executes an account-wide issue settings command.
func runPersonal(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printPersonalUsage(stderr)
		return errors.New("a personal command is required")
	}
	if isHelp(args[0]) {
		printPersonalUsage(stdout)
		return nil
	}

	switch args[0] {
	case "plan":
		options, err := parsePersonalPlanFlags(args[1:], stderr)
		if err != nil {
			if errors.Is(err, flag.ErrHelp) {
				printPersonalUsage(stdout)
				return nil
			}
			return err
		}
		return executePersonalPlan(ctx, options, stdout, stderr)
	case "apply":
		options, err := parsePersonalApplyFlags(args[1:], stderr)
		if err != nil {
			if errors.Is(err, flag.ErrHelp) {
				printPersonalUsage(stdout)
				return nil
			}
			return err
		}
		return executePersonalApply(ctx, options, stdin, stdout, stderr)
	case "import":
		options, err := parsePersonalImportFlags(args[1:], stderr)
		if err != nil {
			if errors.Is(err, flag.ErrHelp) {
				printPersonalUsage(stdout)
				return nil
			}
			return err
		}
		return executePersonalImport(ctx, options, stdin, stdout, stderr)
	case "help":
		printPersonalUsage(stdout)
		return nil
	default:
		printPersonalUsage(stderr)
		return fmt.Errorf("unknown personal command %q", args[0])
	}
}

func isHelp(value string) bool {
	return value == "-h" || value == "-help" || value == "--help"
}

func parsePersonalPlanFlags(args []string, stderr io.Writer) (personalPlanOptions, error) {
	flags := flag.NewFlagSet("personal plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options personalPlanOptions
	flags.StringVar(&options.file, "file", "", "path to a personal YAML or JSON configuration file")
	flags.StringVar(&options.file, "f", "", "path to a personal YAML or JSON configuration file")
	flags.StringVar(&options.format, "format", "text", "output format: text or json")
	flags.BoolVar(&options.ci, "ci", false, "exit 2 when the plan contains a difference, 1 when it is blocked")
	if err := flags.Parse(args); err != nil {
		return personalPlanOptions{}, err
	}
	if flags.NArg() != 0 {
		return personalPlanOptions{}, errors.New("personal plan accepts no positional arguments")
	}
	if options.file == "" {
		return personalPlanOptions{}, errors.New("--file is required")
	}
	if options.format != "text" && options.format != "json" {
		return personalPlanOptions{}, fmt.Errorf("--format must be text or json, got %q", options.format)
	}
	return options, nil
}

func parsePersonalApplyFlags(args []string, stderr io.Writer) (personalApplyOptions, error) {
	flags := flag.NewFlagSet("personal apply", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options personalApplyOptions
	flags.StringVar(&options.file, "file", "", "path to a personal YAML or JSON configuration file")
	flags.StringVar(&options.file, "f", "", "path to a personal YAML or JSON configuration file")
	flags.StringVar(&options.format, "format", "text", "output format: text or json")
	flags.BoolVar(&options.yes, "yes", false, "apply without the remote-change confirmation prompt")
	if err := flags.Parse(args); err != nil {
		return personalApplyOptions{}, err
	}
	if flags.NArg() != 0 {
		return personalApplyOptions{}, errors.New("personal apply accepts no positional arguments")
	}
	if options.file == "" {
		return personalApplyOptions{}, errors.New("--file is required")
	}
	if options.format != "text" && options.format != "json" {
		return personalApplyOptions{}, fmt.Errorf("--format must be text or json, got %q", options.format)
	}
	return options, nil
}

func parsePersonalImportFlags(args []string, stderr io.Writer) (personalImportOptions, error) {
	flags := flag.NewFlagSet("personal import", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := personalImportOptions{format: "yaml"}
	flags.StringVar(&options.format, "format", "yaml", "output format: yaml or json")
	flags.StringVar(&options.into, "into", "", "refresh an existing personal configuration file")
	flags.BoolVar(&options.yes, "yes", false, "skip the --into confirmation prompt")
	if err := flags.Parse(args); err != nil {
		return personalImportOptions{}, err
	}
	flags.Visit(func(f *flag.Flag) { options.formatSet = options.formatSet || f.Name == "format" })
	if flags.NArg() != 0 {
		return personalImportOptions{}, errors.New("personal import accepts no positional arguments")
	}
	if options.format != "yaml" && options.format != "json" {
		return personalImportOptions{}, fmt.Errorf("--format must be yaml or json, got %q", options.format)
	}
	if options.yes && options.into == "" {
		return personalImportOptions{}, errors.New("--yes has no effect without --into")
	}
	return options, nil
}

func executePersonalPlan(ctx context.Context, options personalPlanOptions, stdout, stderr io.Writer) error {
	cfg, engine, err := loadPersonalEngine(options.file)
	if err != nil {
		return err
	}
	plan, err := engine.Build(ctx, cfg)
	if err != nil {
		return err
	}
	if err := writePersonalPlan(stdout, plan, options.format); err != nil {
		return err
	}
	if options.ci {
		if plan.HasBlocked() {
			return errors.New("personal plan is blocked; resolve blocked entries before retrying")
		}
		if count := plan.DriftCount(); count > 0 {
			if _, err := fmt.Fprintf(stderr, "drift detected: %d change(s)\n", count); err != nil {
				return err
			}
			return &DriftError{Changes: count}
		}
	}
	return nil
}

func executePersonalApply(ctx context.Context, options personalApplyOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	cfg, engine, err := loadPersonalEngine(options.file)
	if err != nil {
		return err
	}
	plan, err := engine.Build(ctx, cfg)
	if err != nil {
		return err
	}
	if err := writePersonalPlan(stdout, plan, options.format); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stderr, "Personal settings for %s affect all repositories owned by that account.\n", plan.Account); err != nil {
		return err
	}
	if plan.HasBlocked() {
		return errors.New("personal apply is blocked; resolve blocked entries before retrying")
	}
	if !options.yes {
		confirmed, err := confirm(stdin, stderr, fmt.Sprintf("Apply the planned account-wide changes to %s?", plan.Account))
		if err != nil {
			return err
		}
		if !confirmed {
			return errors.New("personal apply cancelled")
		}
	}
	return engine.Apply(ctx, cfg)
}

func executePersonalImport(ctx context.Context, options personalImportOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	var expectedAccount string
	var original []byte
	var format refresh.Format
	var current *config.PersonalConfig
	if options.into != "" {
		var err error
		original, err = os.ReadFile(options.into)
		if err != nil {
			return fmt.Errorf("read %s: %w", options.into, err)
		}
		format, err = personalIntoFormat(options.format, options.formatSet, original, options.into)
		if err != nil {
			return err
		}
		current, err = config.DecodePersonal(original)
		if err != nil {
			return fmt.Errorf("decode destination %s: %w", options.into, err)
		}
		expectedAccount = current.Account
	}

	client, err := githubapi.NewAuthenticatedClient()
	if err != nil {
		return err
	}
	engine := personal.New(client)
	desired, err := engine.Import(ctx, expectedAccount)
	if err != nil {
		return err
	}
	if options.into == "" {
		return writeImportDocument(stdout, *desired, options.format)
	}

	outcome, err := refresh.RefreshPersonal(original, format, current, desired)
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
	return saveRefresh(options.into, original, outcome.Updated, options.yes, stdin, stdout, stderr)
}

func loadPersonalEngine(path string) (*config.PersonalConfig, *personal.Engine, error) {
	cfg, err := config.LoadPersonal(path)
	if err != nil {
		return nil, nil, err
	}
	client, err := githubapi.NewAuthenticatedClient()
	if err != nil {
		return nil, nil, err
	}
	return cfg, personal.New(client), nil
}

func writePersonalPlan(output io.Writer, plan personal.Plan, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(plan)
	}
	if _, err := fmt.Fprintf(output, "Personal account: %s\n", plan.Account); err != nil {
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

func personalIntoFormat(requested string, formatSet bool, original []byte, path string) (refresh.Format, error) {
	detected := refresh.FormatYAML
	if json.Valid(original) {
		detected = refresh.FormatJSON
	}
	if formatSet && (requested == "json") != (detected == refresh.FormatJSON) {
		return 0, fmt.Errorf("--format %s does not match the format of %s", requested, path)
	}
	return detected, nil
}

func printPersonalUsage(output io.Writer) {
	_, _ = fmt.Fprint(output, `Usage: gh repospec personal <command> [flags]

Commands:
  plan --file <path> [--ci]       Preview personal issue settings.
  apply --file <path> [--yes]     Confirm and apply account-wide issue settings.
  import [--into <path>]          Read or refresh personal issue settings.

Flags:
  -f, --file <path>         Personal configuration path (plan/apply).
  --format <text|json>      Plan output format (import uses yaml|json).
  --ci                      Exit 2 when the plan contains a difference.
  --yes                     Confirm apply or a file refresh without prompting.
`)
}
