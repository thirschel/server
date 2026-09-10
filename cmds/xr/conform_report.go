package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	common "github.com/xregistry/server/common"
)

const (
	conformanceReportSchemaVersion = "1.0.0"
	conformanceReportSchemaID      = "https://xregistry.io/schemas/" +
		"xr_conform_report_v1.schema.json"
	conformanceRunnerName      = "xr conform"
	conformanceLogMessageLimit = 4096

	conformanceDocumentExecution = "execution"
	conformanceDocumentCatalog   = "catalog"
)

type conformanceOutputFormat string

const (
	conformanceOutputText  conformanceOutputFormat = "text"
	conformanceOutputJSON  conformanceOutputFormat = "json"
	conformanceOutputJUnit conformanceOutputFormat = "junit"
)

const (
	conformanceOutcomePassed           = "passed"
	conformanceOutcomeFailed           = "failed"
	conformanceOutcomeWarning          = "warning"
	conformanceOutcomeSkipped          = "skipped"
	conformanceOutcomeDependencyNotRun = "dependency-not-run"
)

const (
	conformanceSelectionRequested      = "requested"
	conformanceSelectionDependencyOnly = "dependency-only"
)

const conformanceTDEntryDescription = "Legacy TD tree-entry counts; " +
	"these are not assertion counts."

type conformanceTargetResult struct {
	RequestedURL        string
	EffectiveURL        string
	DetectedSpecVersion *string
	Root                *TD
	ExitCode            int
}

type conformanceRunnerReport struct {
	Name         string `json:"name"`
	BuildVersion string `json:"buildVersion"`
	Commit       string `json:"commit"`
}

type conformanceProfileReport struct {
	ID             string `json:"id"`
	SpecVersion    string `json:"specVersion"`
	SpecRepository string `json:"specRepository"`
	SpecCommit     string `json:"specCommit"`
	SpecBaseURL    string `json:"specBaseURL"`
}

type conformanceInvocationReport struct {
	RequestedTestIDs  []string                `json:"requestedTestIDs"`
	ResolvedTestIDs   []string                `json:"resolvedTestIDs"`
	CompleteSelection bool                    `json:"completeSelection"`
	Output            conformanceOutputFormat `json:"output"`
	AllowMutations    bool                    `json:"allowMutations"`
}

type conformanceCaseCounts struct {
	Total    int `json:"total"`
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
	Warnings int `json:"warnings"`
	Skipped  int `json:"skipped"`
	NotRun   int `json:"notRun"`
}

type conformanceTDEntryCounts struct {
	Description string `json:"description"`
	Pass        int    `json:"pass"`
	Fail        int    `json:"fail"`
	Warn        int    `json:"warn"`
	Skip        int    `json:"skip"`
}

type conformanceSpecReferenceReport struct {
	Title  string `json:"title"`
	Path   string `json:"path"`
	Anchor string `json:"anchor"`
	URL    string `json:"url"`
}

type conformanceDiagnostic struct {
	Kind        string                  `json:"kind"`
	Name        string                  `json:"name,omitempty"`
	Outcome     string                  `json:"outcome,omitempty"`
	Message     string                  `json:"message,omitempty"`
	Truncated   bool                    `json:"truncated,omitempty"`
	Diagnostics []conformanceDiagnostic `json:"diagnostics,omitempty"`
}

type conformanceCaseReport struct {
	ID                  string                           `json:"id"`
	DisplayName         string                           `json:"displayName"`
	Name                string                           `json:"name"`
	Description         string                           `json:"description"`
	Profile             string                           `json:"profile"`
	Mode                CaseMode                         `json:"mode"`
	Selection           string                           `json:"selection"`
	Dependencies        []string                         `json:"dependencies"`
	BlockedByDependency string                           `json:"blockedByDependency,omitempty"`
	SpecReferences      []conformanceSpecReferenceReport `json:"specReferences"`
	Outcome             string                           `json:"outcome"`
	Diagnostics         []conformanceDiagnostic          `json:"diagnostics"`
}

type conformanceTargetReport struct {
	RequestedURL        string                  `json:"requestedURL"`
	EffectiveURL        string                  `json:"effectiveURL"`
	DetectedSpecVersion *string                 `json:"detectedSpecVersion"`
	ProfileMatch        bool                    `json:"profileMatch"`
	Outcome             string                  `json:"outcome"`
	Cases               []conformanceCaseReport `json:"cases"`
}

type conformanceExecutionReport struct {
	Kind          string                      `json:"kind"`
	Schema        string                      `json:"schema"`
	SchemaVersion string                      `json:"schemaVersion"`
	Runner        conformanceRunnerReport     `json:"runner"`
	Profile       conformanceProfileReport    `json:"profile"`
	Invocation    conformanceInvocationReport `json:"invocation"`
	CaseCounts    conformanceCaseCounts       `json:"caseCounts"`
	TDEntryCounts conformanceTDEntryCounts    `json:"tdEntryCounts"`
	Targets       []conformanceTargetReport   `json:"targets"`
}

type conformanceCatalogCaseReport struct {
	ID             string                           `json:"id"`
	DisplayName    string                           `json:"displayName"`
	Name           string                           `json:"name"`
	Description    string                           `json:"description"`
	Profile        string                           `json:"profile"`
	Mode           CaseMode                         `json:"mode"`
	Dependencies   []string                         `json:"dependencies"`
	SpecReferences []conformanceSpecReferenceReport `json:"specReferences"`
}

type conformanceCatalogReport struct {
	Kind          string                         `json:"kind"`
	Schema        string                         `json:"schema"`
	SchemaVersion string                         `json:"schemaVersion"`
	Runner        conformanceRunnerReport        `json:"runner"`
	Profiles      []conformanceProfileReport     `json:"profiles"`
	Cases         []conformanceCatalogCaseReport `json:"cases"`
}

func parseConformanceOutput(value string) (conformanceOutputFormat, error) {
	if value == "" {
		return conformanceOutputText, nil
	}

	output := conformanceOutputFormat(value)
	switch output {
	case conformanceOutputText, conformanceOutputJSON,
		conformanceOutputJUnit:

		return output, nil
	default:
		return "", fmt.Errorf(
			"invalid --output %q; valid values: text, json, junit",
			value)
	}
}

func buildConformanceExecutionReport(
	results []*conformanceTargetResult,
	options conformOptions,
	profiles []ConformanceProfile,
	catalog []ConformanceCase,
	selection *ConformanceSelection,
) (*conformanceExecutionReport, error) {
	if selection == nil || len(selection.Cases) == 0 {
		return nil, fmt.Errorf(
			"structured conformance output requires a selected catalog")
	}

	profile, err := findConformanceProfile(
		profiles,
		selection.Cases[0].ProfileID,
	)
	if err != nil {
		return nil, err
	}

	report := &conformanceExecutionReport{
		Kind:          conformanceDocumentExecution,
		Schema:        conformanceReportSchemaID,
		SchemaVersion: conformanceReportSchemaVersion,
		Runner:        newConformanceRunnerReport(),
		Profile:       newConformanceProfileReport(profile),
		Invocation: conformanceInvocationReport{
			RequestedTestIDs: copyConformanceStrings(
				selection.RequestedIDs,
			),
			ResolvedTestIDs: make([]string, 0, len(selection.Cases)),
			CompleteSelection: conformanceSelectionIsComplete(
				selection,
				catalog,
				options.allowMutations,
			),
			Output:         options.output,
			AllowMutations: options.allowMutations,
		},
		TDEntryCounts: conformanceTDEntryCounts{
			Description: conformanceTDEntryDescription,
		},
		Targets: make([]conformanceTargetReport, 0, len(results)),
	}

	for _, testCase := range selection.Cases {
		report.Invocation.ResolvedTestIDs = append(
			report.Invocation.ResolvedTestIDs,
			testCase.ID,
		)
	}

	for _, result := range results {
		target, err := buildConformanceTargetReport(
			result,
			options.showLogs,
			profile,
			selection,
		)
		if err != nil {
			return nil, err
		}
		report.Targets = append(report.Targets, target)
		addConformanceCaseCounts(&report.CaseCounts, target.Cases)

		if result.Root != nil {
			report.TDEntryCounts.Pass += result.Root.NumPass
			report.TDEntryCounts.Fail += result.Root.NumFail
			report.TDEntryCounts.Warn += result.Root.NumWarn
			report.TDEntryCounts.Skip += result.Root.NumSkip
		}
	}

	return report, nil
}

func buildConformanceCatalogReport(
	profiles []ConformanceProfile,
	catalog []ConformanceCase,
) (*conformanceCatalogReport, error) {
	if err := validateConformanceCatalog(profiles, catalog); err != nil {
		return nil, err
	}

	report := &conformanceCatalogReport{
		Kind:          conformanceDocumentCatalog,
		Schema:        conformanceReportSchemaID,
		SchemaVersion: conformanceReportSchemaVersion,
		Runner:        newConformanceRunnerReport(),
		Profiles:      make([]conformanceProfileReport, 0, len(profiles)),
		Cases:         make([]conformanceCatalogCaseReport, 0, len(catalog)),
	}

	for _, profile := range profiles {
		report.Profiles = append(
			report.Profiles,
			newConformanceProfileReport(profile),
		)
	}
	for _, testCase := range catalog {
		report.Cases = append(
			report.Cases,
			conformanceCatalogCaseReport{
				ID:          testCase.ID,
				DisplayName: testCase.FunctionName,
				Name:        testCase.Name,
				Description: testCase.Description,
				Profile:     testCase.ProfileID,
				Mode:        testCase.Mode,
				Dependencies: copyConformanceStrings(
					testCase.Dependencies,
				),
				SpecReferences: newConformanceSpecReferenceReports(
					testCase.SpecReferences,
				),
			},
		)
	}

	return report, nil
}

func writeConformanceJSON(out io.Writer, report any) error {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func renderConformanceCatalogJSON(
	out io.Writer,
	profiles []ConformanceProfile,
	catalog []ConformanceCase,
) error {
	report, err := buildConformanceCatalogReport(profiles, catalog)
	if err != nil {
		return err
	}
	return writeConformanceJSON(out, report)
}

func buildConformanceTargetReport(
	result *conformanceTargetResult,
	includeLogs bool,
	profile ConformanceProfile,
	selection *ConformanceSelection,
) (conformanceTargetReport, error) {
	if result == nil || result.Root == nil {
		return conformanceTargetReport{}, fmt.Errorf(
			"conformance target result is incomplete")
	}

	caseTDs := map[string]*TD{}
	for _, entry := range result.Root.Logs {
		if entry.Subtest == nil || entry.Subtest.Case == nil {
			continue
		}
		caseTDs[entry.Subtest.Case.ID] = entry.Subtest
	}

	target := conformanceTargetReport{
		RequestedURL: redactConformanceURL(result.RequestedURL),
		EffectiveURL: redactConformanceURL(result.EffectiveURL),
		ProfileMatch: result.DetectedSpecVersion != nil &&
			*result.DetectedSpecVersion == profile.SpecVersion,
		Cases: make([]conformanceCaseReport, 0, len(selection.Cases)),
	}
	if result.DetectedSpecVersion != nil {
		value := *result.DetectedSpecVersion
		target.DetectedSpecVersion = &value
	}

	for _, testCase := range selection.Cases {
		caseTD := caseTDs[testCase.ID]
		if caseTD == nil {
			return conformanceTargetReport{}, fmt.Errorf(
				"conformance target %q has no result for case %q",
				result.RequestedURL, testCase.ID)
		}

		caseReport := conformanceCaseReport{
			ID:          testCase.ID,
			DisplayName: testCase.FunctionName,
			Name:        testCase.Name,
			Description: testCase.Description,
			Profile:     testCase.ProfileID,
			Mode:        testCase.Mode,
			Selection:   conformanceSelectionDependencyOnly,
			Dependencies: copyConformanceStrings(
				testCase.Dependencies,
			),
			BlockedByDependency: caseTD.DependencyFailed,
			SpecReferences: newConformanceSpecReferenceReports(
				testCase.SpecReferences,
			),
			Outcome: conformanceTDOutcome(caseTD),
			Diagnostics: projectConformanceDiagnostics(
				caseTD,
				includeLogs,
				result,
			),
		}
		if selection.IsRequested(testCase.ID) {
			caseReport.Selection = conformanceSelectionRequested
		}
		target.Cases = append(target.Cases, caseReport)
	}
	target.Outcome = conformanceTargetOutcome(target.Cases)

	return target, nil
}

func newConformanceRunnerReport() conformanceRunnerReport {
	commit := strings.TrimSpace(common.GitCommit)
	if commit == "" {
		commit = "unknown"
	}

	buildVersion := commit
	if buildVersion != "unknown" && len(buildVersion) > 12 {
		buildVersion = buildVersion[:12]
	}

	return conformanceRunnerReport{
		Name:         conformanceRunnerName,
		BuildVersion: buildVersion,
		Commit:       commit,
	}
}

func newConformanceProfileReport(
	profile ConformanceProfile,
) conformanceProfileReport {
	return conformanceProfileReport{
		ID:             profile.ID,
		SpecVersion:    profile.SpecVersion,
		SpecRepository: profile.SpecRepository,
		SpecCommit:     profile.SpecCommit,
		SpecBaseURL:    profile.SpecBaseURL,
	}
}

func newConformanceSpecReferenceReports(
	references []SpecReference,
) []conformanceSpecReferenceReport {
	result := make(
		[]conformanceSpecReferenceReport,
		0,
		len(references),
	)
	for _, reference := range references {
		result = append(result, conformanceSpecReferenceReport{
			Title:  reference.Title,
			Path:   reference.Path,
			Anchor: reference.Anchor,
			URL:    reference.URL,
		})
	}
	return result
}

func findConformanceProfile(
	profiles []ConformanceProfile,
	id string,
) (ConformanceProfile, error) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, nil
		}
	}
	return ConformanceProfile{}, fmt.Errorf(
		"conformance profile %q was not found", id)
}

func conformanceSelectionIsComplete(
	selection *ConformanceSelection,
	catalog []ConformanceCase,
	allowMutations bool,
) bool {
	if selection == nil {
		return false
	}

	permitted := make([]string, 0, len(catalog))
	for i := range catalog {
		if catalog[i].Mode == CaseModeMutation && !allowMutations {
			continue
		}
		permitted = append(permitted, catalog[i].ID)
	}
	if len(selection.Cases) != len(permitted) {
		return false
	}
	for i := range permitted {
		if selection.Cases[i].ID != permitted[i] {
			return false
		}
	}
	return true
}

func addConformanceCaseCounts(
	counts *conformanceCaseCounts,
	cases []conformanceCaseReport,
) {
	for _, testCase := range cases {
		counts.Total++
		switch testCase.Outcome {
		case conformanceOutcomePassed:
			counts.Passed++
		case conformanceOutcomeFailed:
			counts.Failed++
		case conformanceOutcomeWarning:
			counts.Warnings++
		case conformanceOutcomeSkipped:
			counts.Skipped++
		case conformanceOutcomeDependencyNotRun:
			counts.NotRun++
		}
	}
}

func conformanceTargetOutcome(cases []conformanceCaseReport) string {
	hasWarning := false
	hasPassed := false
	for _, testCase := range cases {
		switch testCase.Outcome {
		case conformanceOutcomeFailed:
			return conformanceOutcomeFailed
		case conformanceOutcomeWarning:
			hasWarning = true
		case conformanceOutcomePassed:
			hasPassed = true
		}
	}
	if hasWarning {
		return conformanceOutcomeWarning
	}
	if !hasPassed && len(cases) != 0 {
		return conformanceOutcomeSkipped
	}
	return conformanceOutcomePassed
}

func conformanceTDOutcome(td *TD) string {
	if td == nil {
		return conformanceOutcomeFailed
	}
	if td.DependencyFailed != "" {
		return conformanceOutcomeDependencyNotRun
	}
	if td.Status == FAIL {
		return conformanceOutcomeFailed
	}
	if td.NumWarn != 0 {
		return conformanceOutcomeWarning
	}
	if td.NumSkip != 0 && !conformanceTDHasExplicitPass(td) {
		return conformanceOutcomeSkipped
	}
	return conformanceOutcomePassed
}

func conformanceTDHasExplicitPass(td *TD) bool {
	for _, entry := range td.Logs {
		if entry.Subtest != nil {
			if conformanceTDHasExplicitPass(entry.Subtest) {
				return true
			}
			continue
		}
		if entry.Type == PASS &&
			!strings.HasSuffix(entry.Text, " (cached)") {

			return true
		}
	}
	return false
}

func projectConformanceDiagnostics(
	td *TD,
	includeLogs bool,
	target *conformanceTargetResult,
) []conformanceDiagnostic {
	diagnostics := make([]conformanceDiagnostic, 0, len(td.Logs))
	for _, entry := range td.Logs {
		if entry.Subtest != nil {
			diagnostics = append(diagnostics, conformanceDiagnostic{
				Kind: "group",
				Name: redactConformanceDiagnosticText(
					entry.Subtest.TestName,
					target,
				),
				Outcome: conformanceTDOutcome(entry.Subtest),
				Diagnostics: projectConformanceDiagnostics(
					entry.Subtest,
					includeLogs,
					target,
				),
			})
			continue
		}

		kind := conformanceDiagnosticKind(entry.Type)
		if kind == "" || (entry.Type == LOG && !includeLogs) {
			continue
		}

		message := redactConformanceDiagnosticText(entry.Text, target)
		truncated := false
		if entry.Type == LOG {
			message, truncated = boundConformanceLogMessage(message)
		}
		diagnostics = append(diagnostics, conformanceDiagnostic{
			Kind:      kind,
			Message:   message,
			Truncated: truncated,
		})
	}
	return diagnostics
}

func conformanceDiagnosticKind(status int) string {
	switch status {
	case PASS:
		return "pass"
	case FAIL:
		return "failure"
	case WARN:
		return "warning"
	case SKIP:
		return "skip"
	case LOG:
		return "log"
	case MSG:
		return "message"
	default:
		return ""
	}
}

func boundConformanceLogMessage(message string) (string, bool) {
	runes := []rune(message)
	if len(runes) <= conformanceLogMessageLimit {
		return message, false
	}
	return string(runes[:conformanceLogMessageLimit]), true
}

func copyConformanceStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

var conformanceURLInTextPattern = regexp.MustCompile(
	`https?://[^\s<>"']+`,
)

func redactConformanceDiagnosticText(
	text string,
	target *conformanceTargetResult,
) string {
	if target != nil {
		type replacement struct {
			raw      string
			redacted string
		}
		replacements := []replacement{
			{
				raw:      target.RequestedURL,
				redacted: redactConformanceURL(target.RequestedURL),
			},
			{
				raw:      target.EffectiveURL,
				redacted: redactConformanceURL(target.EffectiveURL),
			},
		}
		if len(replacements[1].raw) > len(replacements[0].raw) {
			replacements[0], replacements[1] =
				replacements[1], replacements[0]
		}
		for _, item := range replacements {
			if item.raw != "" && item.raw != item.redacted {
				text = strings.ReplaceAll(
					text,
					item.raw,
					item.redacted,
				)
			}
		}
	}

	return conformanceURLInTextPattern.ReplaceAllStringFunc(
		text,
		redactConformanceURLMatch,
	)
}

func redactConformanceURLMatch(value string) string {
	suffix := ""
	for len(value) != 0 &&
		strings.ContainsRune(".,;!?)]}", rune(value[len(value)-1])) {

		suffix = value[len(value)-1:] + suffix
		value = value[:len(value)-1]
	}
	return redactConformanceURL(value) + suffix
}

func redactConformanceURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	parseValue := value
	removeAuthorityPrefix := false
	if !strings.Contains(value, "://") &&
		!strings.HasPrefix(value, "//") {

		parseValue = "//" + value
		removeAuthorityPrefix = true
	}

	parsed, err := url.Parse(parseValue)
	if err != nil {
		return "invalid-url"
	}
	parsed.User = nil

	if parsed.RawQuery != "" {
		parts := strings.Split(parsed.RawQuery, "&")
		for i, part := range parts {
			name, _, _ := strings.Cut(part, "=")
			parts[i] = name + "=redacted"
		}
		parsed.RawQuery = strings.Join(parts, "&")
	}

	result := parsed.String()
	if removeAuthorityPrefix {
		result = strings.TrimPrefix(result, "//")
	}
	return result
}
