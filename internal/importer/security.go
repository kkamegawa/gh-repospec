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

	if info.SecurityAndAnalysis != nil {
		statusFields := []struct {
			name   string
			field  *githubapi.SecurityFeature
			assign func(*config.FeatureSetting)
		}{
			{"advancedSecurity", info.SecurityAndAnalysis.AdvancedSecurity, func(value *config.FeatureSetting) { cfg.AdvancedSecurity = value }},
			{"codeSecurity", info.SecurityAndAnalysis.CodeSecurity, func(value *config.FeatureSetting) { cfg.CodeSecurity = value }},
			{"secretScanning", info.SecurityAndAnalysis.SecretScanning, func(value *config.FeatureSetting) { cfg.SecretScanning = value }},
			{"pushProtection", info.SecurityAndAnalysis.SecretScanningPushProtect, func(value *config.FeatureSetting) { cfg.PushProtection = value }},
			{"secretScanningAIDetection", info.SecurityAndAnalysis.SecretScanningAIDetection, func(value *config.FeatureSetting) { cfg.SecretScanningAIDetection = value }},
			{"secretScanningNonProviderPatterns", info.SecurityAndAnalysis.SecretScanningNonProviderPatterns, func(value *config.FeatureSetting) { cfg.SecretScanningNonProviderPatterns = value }},
			{"secretScanningDelegatedAlertDismissal", info.SecurityAndAnalysis.SecretScanningDelegatedAlertDismissal, func(value *config.FeatureSetting) { cfg.SecretScanningDelegatedAlertDismissal = value }},
			{"secretScanningDelegatedBypass", info.SecurityAndAnalysis.SecretScanningDelegatedBypass, func(value *config.FeatureSetting) { cfg.SecretScanningDelegatedBypass = value }},
		}
		for _, status := range statusFields {
			if status.field == nil {
				diagnostics = append(diagnostics, Diagnostic{Resource: "security." + status.name, Message: "GitHub did not report this security setting in the repository response"})
				continue
			}
			status.assign(featureSetting(status.field))
		}
		if options := info.SecurityAndAnalysis.SecretScanningDelegatedBypassOptions; options != nil {
			reviewers := make([]config.DelegatedBypassReviewer, 0, len(options.Reviewers))
			for _, reviewer := range options.Reviewers {
				reviewers = append(reviewers, config.DelegatedBypassReviewer{ReviewerID: reviewer.ReviewerID, ReviewerType: reviewer.ReviewerType, Mode: reviewer.Mode})
			}
			cfg.DelegatedBypassReviewers = &reviewers
		}
	} else {
		for _, name := range []string{"advancedSecurity", "codeSecurity", "secretScanning", "pushProtection", "secretScanningAIDetection", "secretScanningNonProviderPatterns", "secretScanningDelegatedAlertDismissal", "secretScanningDelegatedBypass"} {
			diagnostics = append(diagnostics, Diagnostic{Resource: "security." + name, Message: "GitHub did not report the security_and_analysis block for this repository"})
		}
	}

	if info.SecurityAndAnalysis != nil && info.SecurityAndAnalysis.SecretScanningDelegatedBypass != nil && info.SecurityAndAnalysis.SecretScanningDelegatedBypass.Status == "enabled" && info.SecurityAndAnalysis.SecretScanningDelegatedBypassOptions == nil {
		diagnostics = append(diagnostics, Diagnostic{Resource: "security.delegatedBypassReviewers", Message: "GitHub did not report delegated bypass reviewer options"})
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
	case githubapi.IsForbidden(err):
		// A 403 means code scanning is not enabled for this repository (see
		// docs/adr.md record 9), which says nothing about what a declaration
		// should be, so the field is left unset.
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
	case githubapi.IsForbidden(err) || githubapi.IsNotFound(err):
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

func featureSetting(feature *githubapi.SecurityFeature) *config.FeatureSetting {
	if feature == nil {
		return nil
	}
	return &config.FeatureSetting{Enabled: feature.Status == "enabled"}
}
