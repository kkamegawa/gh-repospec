package importer

import (
	"context"
	"fmt"

	"github.com/kkamegawa/gh-repospec/internal/config"
	"github.com/kkamegawa/gh-repospec/internal/githubapi"
)

// importSecurity fetches the state of every security feature the schema can
// represent. dependencyGraph has no dedicated read endpoint at all,
// secretScanning/pushProtection depend on the security_and_analysis block
// GetRepository already fetched, codeqlDefaultSetup is answered with 403 when
// code scanning is not enabled for the repository, and aiPullRequestScan is
// answered with 403 when Advanced Security is not enabled (or 404 when GitHub
// has no setting to report); each is reported as a diagnostic rather than
// written as a false it was never observed to be, which would never converge.
// A CodeQL default setup 404, by contrast, means not configured and is
// imported as disabled.
func importSecurity(ctx context.Context, reader Reader, repository githubapi.Repository, info githubapi.RepositoryInfo) (*config.SecurityConfig, []Diagnostic, error) {
	cfg := &config.SecurityConfig{}
	var diagnostics []Diagnostic

	alertsEnabled, err := reader.GetVulnerabilityAlertsEnabled(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("get vulnerability alerts status for %s: %w", repository, err)
	}
	cfg.DependabotAlerts = &config.FeatureSetting{Enabled: alertsEnabled}

	securityUpdatesEnabled, err := reader.GetAutomatedSecurityFixesEnabled(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("get automated security fixes status for %s: %w", repository, err)
	}
	cfg.DependabotSecurityUpdates = &config.FeatureSetting{Enabled: securityUpdatesEnabled}

	if info.SecurityAndAnalysis != nil && info.SecurityAndAnalysis.SecretScanning != nil {
		cfg.SecretScanning = &config.FeatureSetting{Enabled: info.SecurityAndAnalysis.SecretScanning.Status == "enabled"}
	} else {
		diagnostics = append(diagnostics, Diagnostic{
			Resource: "security.secretScanning",
			Message:  "GitHub did not report secret scanning status for this repository",
		})
	}

	if info.SecurityAndAnalysis != nil && info.SecurityAndAnalysis.SecretScanningPushProtect != nil {
		cfg.PushProtection = &config.FeatureSetting{Enabled: info.SecurityAndAnalysis.SecretScanningPushProtect.Status == "enabled"}
	} else {
		diagnostics = append(diagnostics, Diagnostic{
			Resource: "security.pushProtection",
			Message:  "GitHub did not report push protection status for this repository",
		})
	}

	codeQL, err := reader.GetCodeQLDefaultSetup(ctx, repository)
	switch {
	case err == nil:
		cfg.CodeQLDefaultSetup = &config.FeatureSetting{Enabled: codeQL.State == "configured"}
	case githubapi.IsNotFound(err):
		// A 404 means CodeQL default setup is unconfigured for this
		// repository, the same "absent means disabled" shape as
		// vulnerability alerts and automated security fixes.
		cfg.CodeQLDefaultSetup = &config.FeatureSetting{Enabled: false}
	case githubapi.IsRefused(err):
		// A 403 means code scanning is not enabled for this repository (see
		// docs/adr.md record 9), which says nothing about what a declaration
		// should be, so the field is left unset. A rate-limited 403 is not
		// such an answer and fails the import below (record 10).
		diagnostics = append(diagnostics, Diagnostic{
			Resource: "security.codeqlDefaultSetup",
			Message:  "GitHub did not report CodeQL default setup status (code scanning may not be enabled for this repository); declare it to manage it going forward",
		})
	default:
		return nil, nil, fmt.Errorf("get CodeQL default setup for %s: %w", repository, err)
	}

	aiScan, err := reader.GetAIScan(ctx, repository)
	switch {
	case err == nil:
		cfg.AIPullRequestScan = &config.FeatureSetting{Enabled: aiScan.PRScan == "enabled"}
	case githubapi.IsRefused(err) || githubapi.IsNotFound(err):
		// Unlike CodeQL's 404, a 403 here says nothing about whether AI Scan
		// is enabled, only that its state could not be read, so the field
		// is left unset instead of being written as disabled.
		diagnostics = append(diagnostics, Diagnostic{
			Resource: "security.aiPullRequestScan",
			Message:  "GitHub did not report AI Scan for pull requests status (Advanced Security may not be enabled); declare it to manage it going forward",
		})
	default:
		return nil, nil, fmt.Errorf("get AI Scan for pull requests status for %s: %w", repository, err)
	}

	privateVulnerabilityReporting, err := reader.GetPrivateVulnerabilityReportingEnabled(ctx, repository)
	if err != nil {
		return nil, nil, fmt.Errorf("get private vulnerability reporting status for %s: %w", repository, err)
	}
	cfg.PrivateVulnerabilityReporting = &config.FeatureSetting{Enabled: privateVulnerabilityReporting}

	diagnostics = append(diagnostics, Diagnostic{
		Resource: "security.dependencyGraph",
		Message:  "GitHub has no read endpoint for dependency graph status; declare it to manage it going forward",
	})

	return cfg, diagnostics, nil
}
