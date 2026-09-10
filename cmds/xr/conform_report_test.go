package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/xregistry/server/cmds/xr/xrlib"
	common "github.com/xregistry/server/common"
)

func TestConformanceTextOutputCompatibility(t *testing.T) {
	server, _ := newObservedConformanceServer(
		t,
		`"specversion": "1.0-rc4"`,
	)
	options := testConformOptions()
	options.testIDs = []string{"core.registry-access"}

	defaultOutput, defaultRC := testConformOutput(
		[]string{server.URL},
		options,
	)
	options.output = conformanceOutputText
	explicitOutput, explicitRC := testConformOutput(
		[]string{server.URL},
		options,
	)

	expected := fmt.Sprintf(`PASS: %s
└─ PASS: TestSniff
Pass: 6   Fail: 0   Warn: 0   Skip: 0
`, server.URL)
	if defaultOutput != expected {
		t.Fatalf("Default text output changed: %s",
			Diff(expected, defaultOutput))
	}
	if explicitOutput != defaultOutput {
		t.Fatalf("Explicit text output changed: %s",
			Diff(defaultOutput, explicitOutput))
	}
	if defaultRC != 0 || explicitRC != defaultRC {
		t.Fatalf("Text exit codes were %d and %d",
			defaultRC, explicitRC)
	}
}

func TestConformanceJSONDocumentsValidateAndAreDeterministic(t *testing.T) {
	oldCommit := common.GitCommit
	common.GitCommit = strings.Repeat("a", 40)
	t.Cleanup(func() {
		common.GitCommit = oldCommit
	})

	server, _ := newObservedConformanceServer(
		t,
		`"specversion": "1.0-rc4"`,
	)
	options := testConformOptions()
	options.output = conformanceOutputJSON
	options.testIDs = []string{"core.registry-access"}

	first := bytes.Buffer{}
	rc, err := runConform([]string{server.URL}, &first, options)
	if err != nil {
		t.Fatal(err)
	}
	if rc != 0 {
		t.Fatalf("JSON execution failed with %d:\n%s", rc, first.String())
	}

	second := bytes.Buffer{}
	rc, err = runConform([]string{server.URL}, &second, options)
	if err != nil {
		t.Fatal(err)
	}
	if rc != 0 {
		t.Fatalf("Second JSON execution failed with %d", rc)
	}
	if first.String() != second.String() {
		t.Fatalf("JSON execution is not deterministic: %s",
			Diff(first.String(), second.String()))
	}
	if strings.Contains(first.String(), "PASS: ") {
		t.Fatalf("Text output leaked into JSON:\n%s", first.String())
	}
	if strings.Contains(first.String(), `"timestamp":`) ||
		strings.Contains(first.String(), `"duration":`) {

		t.Fatalf("Nondeterministic timing metadata leaked into JSON:\n%s",
			first.String())
	}
	if err := validateConformanceDocument(first.Bytes()); err != nil {
		t.Fatalf("Execution report did not validate: %v\n%s",
			err, first.String())
	}

	var execution conformanceExecutionReport
	if err := json.Unmarshal(first.Bytes(), &execution); err != nil {
		t.Fatal(err)
	}
	if execution.Kind != conformanceDocumentExecution ||
		execution.SchemaVersion != conformanceReportSchemaVersion ||
		execution.Profile.ID != coreConformanceProfileID {

		t.Fatalf("Unexpected execution metadata: %#v", execution)
	}
	if execution.Runner.BuildVersion != strings.Repeat("a", 12) ||
		execution.Runner.Commit != common.GitCommit {

		t.Fatalf("Unexpected runner metadata: %#v", execution.Runner)
	}
	if execution.Invocation.Output != conformanceOutputJSON ||
		execution.Invocation.CompleteSelection {

		t.Fatalf("Unexpected invocation: %#v", execution.Invocation)
	}
	if execution.CaseCounts != (conformanceCaseCounts{
		Total:  1,
		Passed: 1,
	}) {
		t.Fatalf("Unexpected logical counts: %#v", execution.CaseCounts)
	}
	if len(execution.Targets) != 1 ||
		execution.Targets[0].DetectedSpecVersion == nil ||
		*execution.Targets[0].DetectedSpecVersion != "1.0-rc4" ||
		!execution.Targets[0].ProfileMatch {

		t.Fatalf("Unexpected target metadata: %#v", execution.Targets)
	}

	catalogFirst := bytes.Buffer{}
	if err := renderConformanceCatalogJSON(
		&catalogFirst,
		ConformanceProfiles,
		ConformanceCatalog,
	); err != nil {
		t.Fatal(err)
	}
	catalogSecond := bytes.Buffer{}
	if err := renderConformanceCatalogJSON(
		&catalogSecond,
		ConformanceProfiles,
		ConformanceCatalog,
	); err != nil {
		t.Fatal(err)
	}
	if catalogFirst.String() != catalogSecond.String() {
		t.Fatalf("Catalog JSON is not deterministic: %s",
			Diff(catalogFirst.String(), catalogSecond.String()))
	}
	if err := validateConformanceDocument(catalogFirst.Bytes()); err != nil {
		t.Fatalf("Catalog report did not validate: %v\n%s",
			err, catalogFirst.String())
	}

	var catalogDocument map[string]any
	if err := json.Unmarshal(catalogFirst.Bytes(), &catalogDocument); err != nil {
		t.Fatal(err)
	}
	catalogDocument["targets"] = []any{}
	invalid, err := json.Marshal(catalogDocument)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateConformanceDocument(invalid); err == nil {
		t.Fatal("Schema accepted a catalog document with execution fields")
	}
}

func TestConformanceJSONGolden(t *testing.T) {
	report := goldenConformanceExecutionReport(conformanceOutputJSON)
	out := bytes.Buffer{}
	if err := writeConformanceJSON(&out, report); err != nil {
		t.Fatal(err)
	}

	const expected = `{
  "kind": "execution",
  "schema": "https://xregistry.io/schemas/xr_conform_report_v1.schema.json",
  "schemaVersion": "1.0.0",
  "runner": {
    "name": "xr conform",
    "buildVersion": "0123456789ab",
    "commit": "0123456789abcdef0123456789abcdef01234567"
  },
  "profile": {
    "id": "xregistry-core-1.0-rc4",
    "specVersion": "1.0-rc4",
    "specRepository": "https://github.com/xregistry/spec",
    "specCommit": "d2433a8c726ab096303bd943a4fc6691925f7910",
    "specBaseURL": "https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/"
  },
  "invocation": {
    "requestedTestIDs": [
      "core.registry-access"
    ],
    "resolvedTestIDs": [
      "core.registry-access"
    ],
    "completeSelection": false,
    "output": "json",
    "allowMutations": false
  },
  "caseCounts": {
    "total": 1,
    "passed": 1,
    "failed": 0,
    "warnings": 0,
    "skipped": 0,
    "notRun": 0
  },
  "tdEntryCounts": {
    "description": "Legacy TD tree-entry counts; these are not assertion counts.",
    "pass": 2,
    "fail": 0,
    "warn": 0,
    "skip": 0
  },
  "targets": [
    {
      "requestedURL": "https://example.com",
      "effectiveURL": "https://example.com",
      "detectedSpecVersion": "1.0-rc4",
      "profileMatch": true,
      "outcome": "passed",
      "cases": [
        {
          "id": "core.registry-access",
          "displayName": "TestSniff",
          "name": "Registry access",
          "description": "Verify registry access.",
          "profile": "xregistry-core-1.0-rc4",
          "mode": "read-only",
          "selection": "requested",
          "dependencies": [],
          "specReferences": [
            {
              "title": "Registry Entity",
              "path": "core/spec.md",
              "anchor": "registry-entity",
              "url": "https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/spec.md#registry-entity"
            }
          ],
          "outcome": "passed",
          "diagnostics": [
            {
              "kind": "pass",
              "message": "ok"
            }
          ]
        }
      ]
    }
  ]
}
`
	if out.String() != expected {
		t.Fatalf("JSON golden changed: %s", Diff(expected, out.String()))
	}
}

func TestConformanceStructuredTargetsPreserveOrderAndExecuteOnce(
	t *testing.T,
) {
	firstServer, firstRequests := newObservedConformanceServer(
		t,
		`"specversion": "1.0-rc4"`,
	)
	secondServer, secondRequests := newObservedConformanceServer(
		t,
		`"specversion": "1.0-rc4"`,
	)

	options := testConformOptions()
	options.output = conformanceOutputJSON
	options.testIDs = []string{"core.registry-access"}
	out := bytes.Buffer{}
	rc, err := runConform(
		[]string{
			firstServer.URL,
			secondServer.URL,
			firstServer.URL,
		},
		&out,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if rc != 0 {
		t.Fatalf("Multi-target JSON failed with %d:\n%s", rc, out.String())
	}

	var report conformanceExecutionReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	want := []string{
		firstServer.URL,
		secondServer.URL,
		firstServer.URL,
	}
	if len(report.Targets) != len(want) {
		t.Fatalf("Got %d targets, want %d", len(report.Targets), len(want))
	}
	for i, target := range report.Targets {
		if target.RequestedURL != want[i] {
			t.Fatalf("Target %d is %q, want %q",
				i, target.RequestedURL, want[i])
		}
	}
	if firstRequests.Count("/") != 2 ||
		secondRequests.Count("/") != 1 {

		t.Fatalf("Targets were executed more than once: first=%d second=%d",
			firstRequests.Count("/"), secondRequests.Count("/"))
	}

	options.output = conformanceOutputJUnit
	out.Reset()
	rc, err = runConform(
		[]string{
			firstServer.URL,
			secondServer.URL,
			firstServer.URL,
		},
		&out,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if rc != 0 {
		t.Fatalf("Multi-target JUnit failed with %d:\n%s",
			rc, out.String())
	}
	if strings.Contains(out.String(), "PASS: ") ||
		strings.Contains(out.String(), "Pass: ") {

		t.Fatalf("Text output leaked into JUnit:\n%s", out.String())
	}
	var junit conformanceJUnitTestSuites
	if err := xml.Unmarshal(out.Bytes(), &junit); err != nil {
		t.Fatal(err)
	}
	if len(junit.Suites) != len(want) {
		t.Fatalf("Got %d suites, want %d",
			len(junit.Suites), len(want))
	}
	for i, suite := range junit.Suites {
		if suite.Name != want[i] {
			t.Fatalf("Suite %d is %q, want %q",
				i, suite.Name, want[i])
		}
	}
	if firstRequests.Count("/") != 4 ||
		secondRequests.Count("/") != 2 {

		t.Fatalf("JUnit targets were executed more than once: first=%d second=%d",
			firstRequests.Count("/"), secondRequests.Count("/"))
	}
}

func TestConformanceReportRedactsURLsAndOmitsConfiguredHeaders(
	t *testing.T,
) {
	rawURL := "https://alice:password@example.com/root" +
		"?token=s3cr3t&empty=&token=second#fragment"
	catalog := singleConformanceReportCase(
		TestFn(conformanceReportRedactionProbe),
	)
	options := testConformOptions()
	options.output = conformanceOutputJSON
	options.showLogs = true

	out := bytes.Buffer{}
	rc, err := runConformWithCatalog(
		[]string{rawURL},
		&out,
		options,
		copyConformanceProfiles(),
		catalog,
	)
	if err != nil {
		t.Fatal(err)
	}
	if rc != 0 {
		t.Fatalf("Redaction probe failed with %d:\n%s", rc, out.String())
	}
	for _, secret := range []string{
		"alice",
		"password",
		"s3cr3t",
		"second",
	} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("Report leaked %q:\n%s", secret, out.String())
		}
	}
	wantURL := "https://example.com/root" +
		"?token=redacted&empty=redacted&token=redacted#fragment"
	if strings.Count(out.String(), wantURL) < 2 {
		t.Fatalf("Redacted URL missing from fields/diagnostics:\n%s",
			out.String())
	}

	const headerSecret = "Bearer configured-header-secret"
	oldHeaders := xrlib.HTTPHeaders
	xrlib.HTTPHeaders = map[string]string{
		"Authorization":       headerSecret,
		"Cookie":              "session=configured-cookie-secret",
		"X-API-Key":           "configured-api-key-secret",
		"Proxy-Authorization": "configured-proxy-secret",
	}
	t.Cleanup(func() {
		xrlib.HTTPHeaders = oldHeaders
	})

	headerObserved := false
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			headerObserved = r.Header.Get("Authorization") == headerSecret
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{
  "specversion": "1.0-rc4",
  "registryid": "test",
  "self": "http://%s/",
  "xid": "/",
  "epoch": 1,
  "createdat": "2026-01-01T00:00:00Z",
  "modifiedat": "2026-01-01T00:00:00Z"
}`, r.Host)
		}))
	t.Cleanup(server.Close)

	headerOptions := testConformOptions()
	headerOptions.output = conformanceOutputJSON
	headerOptions.testIDs = []string{"core.registry-access"}
	headerOut := bytes.Buffer{}
	rc, err = runConform(
		[]string{server.URL},
		&headerOut,
		headerOptions,
	)
	if err != nil {
		t.Fatal(err)
	}
	if rc != 0 || !headerObserved {
		t.Fatalf("Configured-header run failed: rc=%d observed=%v\n%s",
			rc, headerObserved, headerOut.String())
	}
	for _, secret := range []string{
		headerSecret,
		"configured-cookie-secret",
		"configured-api-key-secret",
		"configured-proxy-secret",
		"Authorization",
		"Proxy-Authorization",
		"X-API-Key",
	} {
		if strings.Contains(headerOut.String(), secret) {
			t.Fatalf("Report serialized configured header data %q:\n%s",
				secret, headerOut.String())
		}
	}
}

func TestConformanceReportLogFilteringAndTruncation(t *testing.T) {
	oldMessage := conformanceReportTestLogMessage
	conformanceReportTestLogMessage = strings.Repeat(
		"界",
		conformanceLogMessageLimit+17,
	)
	t.Cleanup(func() {
		conformanceReportTestLogMessage = oldMessage
	})

	catalog := singleConformanceReportCase(
		TestFn(conformanceReportLogProbe),
	)
	options := testConformOptions()
	options.output = conformanceOutputJSON

	withoutLogs := runConformanceReportForTest(
		t,
		[]string{"https://example.com"},
		options,
		catalog,
	)
	diagnostics := withoutLogs.Targets[0].Cases[0].Diagnostics
	assertConformanceDiagnosticKinds(
		t,
		diagnostics,
		[]string{"message", "warning", "skip", "pass"},
	)

	options.showLogs = true
	withLogs := runConformanceReportForTest(
		t,
		[]string{"https://example.com"},
		options,
		catalog,
	)
	diagnostics = withLogs.Targets[0].Cases[0].Diagnostics
	assertConformanceDiagnosticKinds(
		t,
		diagnostics,
		[]string{"log", "message", "warning", "skip", "pass"},
	)
	logEntry := diagnostics[0]
	if !logEntry.Truncated ||
		len([]rune(logEntry.Message)) != conformanceLogMessageLimit {

		t.Fatalf("Log was not deterministically truncated: %#v", logEntry)
	}
	if withLogs.Targets[0].Cases[0].Outcome !=
		conformanceOutcomeWarning {

		t.Fatalf("Warning case outcome is %q",
			withLogs.Targets[0].Cases[0].Outcome)
	}
}

func TestConformanceUnknownCommitIsExplicit(t *testing.T) {
	oldCommit := common.GitCommit
	common.GitCommit = ""
	t.Cleanup(func() {
		common.GitCommit = oldCommit
	})

	runner := newConformanceRunnerReport()
	if runner.Commit != "unknown" || runner.BuildVersion != "unknown" {
		t.Fatalf("Unknown commit metadata was %#v", runner)
	}
}

func TestConformanceJUnitMappingsOrderAndXMLSanitization(t *testing.T) {
	report := junitMappingConformanceReport()
	out := bytes.Buffer{}
	if err := writeConformanceJUnit(&out, report); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), xml.Header) {
		t.Fatalf("JUnit is missing the UTF-8 XML declaration:\n%s",
			out.String())
	}
	if strings.ContainsRune(out.String(), '\x01') {
		t.Fatalf("JUnit retained an invalid XML control:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "\uFFFD") {
		t.Fatalf("JUnit did not mark the invalid XML control:\n%s",
			out.String())
	}
	if strings.Contains(out.String(), "time=") ||
		strings.Contains(out.String(), "timestamp=") {

		t.Fatalf("JUnit included timing metadata:\n%s", out.String())
	}

	var document conformanceJUnitTestSuites
	if err := xml.Unmarshal(out.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.Tests != 6 ||
		document.Failures != 1 ||
		document.Skipped != 2 {

		t.Fatalf("Unexpected JUnit counts: %#v", document)
	}
	if len(document.Suites) != 2 ||
		document.Suites[0].Name != "https://first.example" ||
		document.Suites[1].Name != "https://second.example" {

		t.Fatalf("Unexpected suite order: %#v", document.Suites)
	}

	wantCases := []string{
		"core.pass",
		"core.fail",
		"core.dependency",
		"core.warning",
		"core.skip",
	}
	firstSuite := document.Suites[0]
	if len(firstSuite.TestCases) != len(wantCases) {
		t.Fatalf("Got %d testcases, want %d",
			len(firstSuite.TestCases), len(wantCases))
	}
	seen := map[string]bool{}
	for i, testCase := range firstSuite.TestCases {
		if testCase.Name != wantCases[i] {
			t.Fatalf("Testcase %d is %q, want %q",
				i, testCase.Name, wantCases[i])
		}
		if seen[testCase.Name] {
			t.Fatalf("Duplicate testcase %q", testCase.Name)
		}
		seen[testCase.Name] = true
	}

	if firstSuite.TestCases[0].Failure != nil ||
		firstSuite.TestCases[0].Skipped != nil {

		t.Fatal("Passing testcase was not passing")
	}
	if firstSuite.TestCases[1].Failure == nil ||
		!strings.Contains(
			firstSuite.TestCases[1].Failure.Text,
			"failure: failed",
		) {

		t.Fatalf("Failure mapping is incomplete: %#v",
			firstSuite.TestCases[1])
	}
	if firstSuite.TestCases[2].Skipped == nil ||
		firstSuite.TestCases[2].Skipped.Message !=
			"Dependency core.fail failed" {

		t.Fatalf("Dependency mapping is incomplete: %#v",
			firstSuite.TestCases[2])
	}
	if firstSuite.TestCases[3].Failure != nil ||
		firstSuite.TestCases[3].Skipped != nil ||
		!strings.Contains(
			firstSuite.TestCases[3].SystemOut,
			"warning: warn�ing",
		) ||
		!strings.Contains(
			firstSuite.TestCases[3].SystemOut,
			"skip: partial skip",
		) {

		t.Fatalf("Warning/partial skip mapping is incomplete: %#v",
			firstSuite.TestCases[3])
	}
	if firstSuite.TestCases[4].Skipped == nil {
		t.Fatalf("Skipped testcase was not mapped: %#v",
			firstSuite.TestCases[4])
	}
	if got := junitPropertyValue(
		firstSuite.TestCases[2].Properties,
		"selection",
	); got != conformanceSelectionDependencyOnly {

		t.Fatalf("Dependency selection property is %q", got)
	}
	if got := junitPropertyValue(
		firstSuite.TestCases[0].Properties,
		"display-name",
	); got != "TestPass" {

		t.Fatalf("Display name property is %q", got)
	}
}

func TestConformanceJUnitGolden(t *testing.T) {
	report := goldenConformanceExecutionReport(conformanceOutputJUnit)
	out := bytes.Buffer{}
	if err := writeConformanceJUnit(&out, report); err != nil {
		t.Fatal(err)
	}

	const expected = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites name="xr conform" tests="1" failures="0" skipped="0">
  <testsuite name="https://example.com" tests="1" failures="0" skipped="0">
    <properties>
      <property name="schema" value="https://xregistry.io/schemas/xr_conform_report_v1.schema.json"></property>
      <property name="schema-version" value="1.0.0"></property>
      <property name="runner" value="xr conform"></property>
      <property name="build-version" value="0123456789ab"></property>
      <property name="commit" value="0123456789abcdef0123456789abcdef01234567"></property>
      <property name="profile" value="xregistry-core-1.0-rc4"></property>
      <property name="spec-version" value="1.0-rc4"></property>
      <property name="spec-commit" value="d2433a8c726ab096303bd943a4fc6691925f7910"></property>
      <property name="requested-url" value="https://example.com"></property>
      <property name="effective-url" value="https://example.com"></property>
      <property name="detected-spec-version" value="1.0-rc4"></property>
      <property name="profile-match" value="true"></property>
      <property name="outcome" value="passed"></property>
      <property name="output" value="junit"></property>
      <property name="allow-mutations" value="false"></property>
      <property name="complete-selection" value="false"></property>
      <property name="requested-test-ids" value="core.registry-access"></property>
      <property name="resolved-test-ids" value="core.registry-access"></property>
    </properties>
    <testcase name="core.registry-access">
      <properties>
        <property name="display-name" value="TestSniff"></property>
        <property name="name" value="Registry access"></property>
        <property name="description" value="Verify registry access."></property>
        <property name="profile" value="xregistry-core-1.0-rc4"></property>
        <property name="mode" value="read-only"></property>
        <property name="selection" value="requested"></property>
        <property name="dependencies" value=""></property>
        <property name="outcome" value="passed"></property>
        <property name="warnings" value="0"></property>
        <property name="skips" value="0"></property>
        <property name="specification.1" value="https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/spec.md#registry-entity"></property>
      </properties>
      <system-out>pass: ok&#xA;</system-out>
    </testcase>
  </testsuite>
</testsuites>
`
	if out.String() != expected {
		t.Fatalf("JUnit golden changed: %s", Diff(expected, out.String()))
	}
}

func TestConformanceStructuredFlagValidationIsOffline(t *testing.T) {
	server, requests := newObservedConformanceServer(
		t,
		`"specversion": "1.0-rc4"`,
	)

	tests := []struct {
		name    string
		options conformOptions
		want    string
	}{
		{
			name: "invalid output",
			options: conformOptions{
				output: "yaml",
			},
			want: "invalid --output",
		},
		{
			name: "explicit depth",
			options: conformOptions{
				output:        conformanceOutputJSON,
				depthExplicit: true,
			},
			want: "--depth cannot be combined",
		},
		{
			name: "explicit nowrap",
			options: conformOptions{
				output:         conformanceOutputJUnit,
				nowrapExplicit: true,
			},
			want: "--nowrap cannot be combined",
		},
		{
			name: "hidden run",
			options: conformOptions{
				output:  conformanceOutputJSON,
				runFunc: "TestTDAllPass",
			},
			want: "--run only supports",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out := bytes.Buffer{}
			rc, err := runConform(
				[]string{server.URL},
				&out,
				test.options,
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Expected %q, got %v", test.want, err)
			}
			if rc != 0 || out.Len() != 0 {
				t.Fatalf("Validation wrote output: rc=%d out=%q",
					rc, out.String())
			}
		})
	}

	cmd := newConformanceInvocationTestCommand()
	if err := cmd.Flags().Set("list-tests", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("output", "junit"); err != nil {
		t.Fatal(err)
	}
	err := validateConformInvocation(cmd, nil, true)
	if err == nil ||
		!strings.Contains(err.Error(), "--list-tests") ||
		!strings.Contains(err.Error(), "junit") {

		t.Fatalf("Unexpected list/JUnit error: %v", err)
	}
	if requests.Total() != 0 {
		t.Fatalf("Invalid flags sent %d requests", requests.Total())
	}
}

func TestConformanceListJSONIsOffline(t *testing.T) {
	server, requests := newMethodProbeServer(t)
	oldServer := GetServer()
	XRConfig.Set("server.url", server.URL)
	t.Cleanup(func() {
		XRConfig.Set("server.url", oldServer)
	})

	cmd := newConformanceInvocationTestCommand()
	if err := cmd.Flags().Set("list-tests", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatal(err)
	}

	output := captureTestStdout(t, func() {
		conformFunc(cmd, nil)
	})
	if err := validateConformanceDocument([]byte(output)); err != nil {
		t.Fatalf("Catalog listing did not validate: %v\n%s", err, output)
	}
	if requests.Total() != 0 {
		t.Fatalf("--list-tests --output json sent %d requests",
			requests.Total())
	}
}

func TestConformanceOutputPreservesExitStatus(t *testing.T) {
	server, requests := newObservedConformanceServer(
		t,
		`"specversion": "1.0-rc5"`,
	)
	var expectedRC int
	for _, output := range []conformanceOutputFormat{
		conformanceOutputText,
		conformanceOutputJSON,
		conformanceOutputJUnit,
	} {
		t.Run(string(output), func(t *testing.T) {
			options := testConformOptions()
			options.output = output
			out := bytes.Buffer{}
			rc, err := runConform(
				[]string{server.URL},
				&out,
				options,
			)
			if err != nil {
				t.Fatal(err)
			}
			if expectedRC == 0 {
				expectedRC = rc
			}
			if rc != expectedRC || rc != FAIL {
				t.Fatalf("%s exit code is %d, want %d",
					output, rc, FAIL)
			}
			switch output {
			case conformanceOutputJSON:
				if err := validateConformanceDocument(out.Bytes()); err != nil {
					t.Fatalf("Failed JSON did not validate: %v", err)
				}
				var report conformanceExecutionReport
				if err := json.Unmarshal(out.Bytes(), &report); err != nil {
					t.Fatal(err)
				}
				if report.Targets[0].Cases[0].Outcome !=
					conformanceOutcomeFailed ||
					report.Targets[0].Cases[1].Outcome !=
						conformanceOutcomeDependencyNotRun {

					t.Fatalf("Unexpected failure outcomes: %#v",
						report.Targets[0].Cases[:2])
				}
				if !report.Invocation.CompleteSelection ||
					len(report.Invocation.RequestedTestIDs) !=
						len(ConformanceCatalog) ||
					len(report.Invocation.ResolvedTestIDs) !=
						len(ConformanceCatalog) {

					t.Fatalf("Unexpected complete selection: %#v",
						report.Invocation)
				}
			case conformanceOutputJUnit:
				var document conformanceJUnitTestSuites
				if err := xml.Unmarshal(out.Bytes(), &document); err != nil {
					t.Fatal(err)
				}
				if document.Failures != 1 ||
					document.Skipped != len(ConformanceCatalog)-1 ||
					len(document.Suites) != 1 ||
					len(document.Suites[0].TestCases) !=
						len(ConformanceCatalog) {

					t.Fatalf("Unexpected failed JUnit counts: %#v",
						document)
				}
				seen := map[string]bool{}
				for _, testCase := range document.Suites[0].TestCases {
					if seen[testCase.Name] {
						t.Fatalf("Duplicate JUnit testcase %q",
							testCase.Name)
					}
					seen[testCase.Name] = true
				}
			}
		})
	}
	if requests.Total() != 3 {
		t.Fatalf("Output formats changed execution count: %d requests",
			requests.Total())
	}

	options := testConformOptions()
	options.output = conformanceOutputJSON
	out := bytes.Buffer{}
	rc, err := runConform(
		[]string{server.URL, server.URL},
		&out,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if rc != 2*FAIL {
		t.Fatalf("Multi-target exit code is %d, want %d", rc, 2*FAIL)
	}
}

func goldenConformanceExecutionReport(
	output conformanceOutputFormat,
) *conformanceExecutionReport {
	detected := "1.0-rc4"
	reference := conformanceSpecReferenceReport{
		Title:  "Registry Entity",
		Path:   "core/spec.md",
		Anchor: "registry-entity",
		URL: "https://github.com/xregistry/spec/blob/" +
			coreSpecCommit + "/core/spec.md#registry-entity",
	}
	return &conformanceExecutionReport{
		Kind:          conformanceDocumentExecution,
		Schema:        conformanceReportSchemaID,
		SchemaVersion: conformanceReportSchemaVersion,
		Runner: conformanceRunnerReport{
			Name:         conformanceRunnerName,
			BuildVersion: "0123456789ab",
			Commit:       "0123456789abcdef0123456789abcdef01234567",
		},
		Profile: newConformanceProfileReport(coreConformanceProfile),
		Invocation: conformanceInvocationReport{
			RequestedTestIDs:  []string{"core.registry-access"},
			ResolvedTestIDs:   []string{"core.registry-access"},
			CompleteSelection: false,
			Output:            output,
			AllowMutations:    false,
		},
		CaseCounts: conformanceCaseCounts{
			Total:  1,
			Passed: 1,
		},
		TDEntryCounts: conformanceTDEntryCounts{
			Description: conformanceTDEntryDescription,
			Pass:        2,
		},
		Targets: []conformanceTargetReport{
			{
				RequestedURL:        "https://example.com",
				EffectiveURL:        "https://example.com",
				DetectedSpecVersion: &detected,
				ProfileMatch:        true,
				Outcome:             conformanceOutcomePassed,
				Cases: []conformanceCaseReport{
					{
						ID:           "core.registry-access",
						DisplayName:  "TestSniff",
						Name:         "Registry access",
						Description:  "Verify registry access.",
						Profile:      coreConformanceProfileID,
						Mode:         CaseModeReadOnly,
						Selection:    conformanceSelectionRequested,
						Dependencies: []string{},
						SpecReferences: []conformanceSpecReferenceReport{
							reference,
						},
						Outcome: conformanceOutcomePassed,
						Diagnostics: []conformanceDiagnostic{
							{
								Kind:    "pass",
								Message: "ok",
							},
						},
					},
				},
			},
		},
	}
}

func junitMappingConformanceReport() *conformanceExecutionReport {
	report := goldenConformanceExecutionReport(conformanceOutputJUnit)
	report.Invocation.RequestedTestIDs = []string{
		"core.pass",
		"core.fail",
		"core.warning",
		"core.skip",
	}
	report.Invocation.ResolvedTestIDs = []string{
		"core.pass",
		"core.fail",
		"core.dependency",
		"core.warning",
		"core.skip",
	}
	report.Targets = []conformanceTargetReport{
		{
			RequestedURL: "https://first.example",
			EffectiveURL: "https://first.example",
			ProfileMatch: true,
			Outcome:      conformanceOutcomeFailed,
			Cases: []conformanceCaseReport{
				junitMappingCase(
					"core.pass",
					"TestPass",
					conformanceSelectionRequested,
					conformanceOutcomePassed,
					conformanceDiagnostic{
						Kind:    "pass",
						Message: "passed",
					},
				),
				junitMappingCase(
					"core.fail",
					"TestFail",
					conformanceSelectionRequested,
					conformanceOutcomeFailed,
					conformanceDiagnostic{
						Kind:    "failure",
						Message: "failed",
					},
				),
				func() conformanceCaseReport {
					testCase := junitMappingCase(
						"core.dependency",
						"TestDependency",
						conformanceSelectionDependencyOnly,
						conformanceOutcomeDependencyNotRun,
						conformanceDiagnostic{
							Kind:    "failure",
							Message: "TestFail (cached)",
						},
						conformanceDiagnostic{
							Kind:    "message",
							Message: "Dependency failed",
						},
					)
					testCase.Dependencies = []string{"core.fail"}
					testCase.BlockedByDependency = "core.fail"
					return testCase
				}(),
				junitMappingCase(
					"core.warning",
					"TestWarning",
					conformanceSelectionRequested,
					conformanceOutcomeWarning,
					conformanceDiagnostic{
						Kind:    "warning",
						Message: "warn\x01ing",
					},
					conformanceDiagnostic{
						Kind:    "skip",
						Message: "partial skip",
					},
				),
				junitMappingCase(
					"core.skip",
					"TestSkip",
					conformanceSelectionRequested,
					conformanceOutcomeSkipped,
					conformanceDiagnostic{
						Kind:    "skip",
						Message: "not applicable",
					},
				),
			},
		},
		{
			RequestedURL: "https://second.example",
			EffectiveURL: "https://second.example",
			ProfileMatch: true,
			Outcome:      conformanceOutcomePassed,
			Cases: []conformanceCaseReport{
				junitMappingCase(
					"core.pass",
					"TestPass",
					conformanceSelectionRequested,
					conformanceOutcomePassed,
					conformanceDiagnostic{
						Kind:    "pass",
						Message: "passed",
					},
				),
			},
		},
	}
	return report
}

func junitMappingCase(
	id string,
	displayName string,
	selection string,
	outcome string,
	diagnostics ...conformanceDiagnostic,
) conformanceCaseReport {
	return conformanceCaseReport{
		ID:           id,
		DisplayName:  displayName,
		Name:         strings.TrimPrefix(displayName, "Test"),
		Description:  "JUnit mapping probe.",
		Profile:      coreConformanceProfileID,
		Mode:         CaseModeReadOnly,
		Selection:    selection,
		Dependencies: []string{},
		SpecReferences: []conformanceSpecReferenceReport{
			{
				Title:  "Registry Entity",
				Path:   "core/spec.md",
				Anchor: "registry-entity",
				URL: "https://github.com/xregistry/spec/blob/" +
					coreSpecCommit + "/core/spec.md#registry-entity",
			},
		},
		Outcome:     outcome,
		Diagnostics: diagnostics,
	}
}

func singleConformanceReportCase(testFn TestFn) []ConformanceCase {
	return []ConformanceCase{
		{
			ID:           "core.report-probe",
			FunctionName: testFn.DisplayName(),
			Name:         "Report probe",
			Description:  "Exercise deterministic report projection.",
			ProfileID:    coreConformanceProfileID,
			Mode:         CaseModeReadOnly,
			Test:         testFn,
			SpecReferences: []SpecReference{
				newSpecReference(
					coreConformanceProfile,
					"core/http.md",
					"registry-http-apis",
					"Registry HTTP APIs",
				),
			},
		},
	}
}

func runConformanceReportForTest(
	t *testing.T,
	servers []string,
	options conformOptions,
	catalog []ConformanceCase,
) conformanceExecutionReport {
	t.Helper()
	out := bytes.Buffer{}
	rc, err := runConformWithCatalog(
		servers,
		&out,
		options,
		copyConformanceProfiles(),
		catalog,
	)
	if err != nil {
		t.Fatal(err)
	}
	if rc != 0 {
		t.Fatalf("Report probe failed with %d:\n%s", rc, out.String())
	}

	var report conformanceExecutionReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func validateConformanceDocument(data []byte) error {
	schemaPath := filepath.Join(
		"..",
		"..",
		"docs",
		"xr_conform_report_v1.schema.json",
	)
	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		return err
	}
	schemaDocument, err := jsonschema.UnmarshalJSON(
		bytes.NewReader(schemaData),
	)
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(
		conformanceReportSchemaID,
		schemaDocument,
	); err != nil {
		return err
	}
	schema, err := compiler.Compile(conformanceReportSchemaID)
	if err != nil {
		return err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return schema.Validate(document)
}

func assertConformanceDiagnosticKinds(
	t *testing.T,
	diagnostics []conformanceDiagnostic,
	want []string,
) {
	t.Helper()
	got := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		got = append(got, diagnostic.Kind)
	}
	assertStringsEqual(t, got, want)
}

func junitPropertyValue(
	properties conformanceJUnitProperties,
	name string,
) string {
	for _, property := range properties.Values {
		if property.Name == name {
			return property.Value
		}
	}
	return ""
}

var conformanceReportTestLogMessage string

func conformanceReportRedactionProbe(td *TD) {
	td.Log("Registry URL: %s", td.GetRegistry().GetServerURL())
	td.Pass("Redaction probe passed")
}

func conformanceReportLogProbe(td *TD) {
	td.Log("%s", conformanceReportTestLogMessage)
	td.Msg("Always-visible message")
	td.Warn("Warning diagnostic")
	td.Skip("Partial skip diagnostic")
	td.Pass("Passing diagnostic")
}
