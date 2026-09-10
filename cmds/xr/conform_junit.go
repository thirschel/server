package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type conformanceJUnitTestSuites struct {
	XMLName  xml.Name                    `xml:"testsuites"`
	Name     string                      `xml:"name,attr"`
	Tests    int                         `xml:"tests,attr"`
	Failures int                         `xml:"failures,attr"`
	Skipped  int                         `xml:"skipped,attr"`
	Suites   []conformanceJUnitTestSuite `xml:"testsuite"`
}

type conformanceJUnitTestSuite struct {
	Name       string                     `xml:"name,attr"`
	Tests      int                        `xml:"tests,attr"`
	Failures   int                        `xml:"failures,attr"`
	Skipped    int                        `xml:"skipped,attr"`
	Properties conformanceJUnitProperties `xml:"properties"`
	TestCases  []conformanceJUnitTestCase `xml:"testcase"`
}

type conformanceJUnitTestCase struct {
	Name       string                     `xml:"name,attr"`
	Properties conformanceJUnitProperties `xml:"properties"`
	Failure    *conformanceJUnitFailure   `xml:"failure,omitempty"`
	Skipped    *conformanceJUnitSkipped   `xml:"skipped,omitempty"`
	SystemOut  string                     `xml:"system-out,omitempty"`
}

type conformanceJUnitProperties struct {
	Values []conformanceJUnitProperty `xml:"property"`
}

type conformanceJUnitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type conformanceJUnitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type conformanceJUnitSkipped struct {
	Message string `xml:"message,attr"`
}

func writeConformanceJUnit(
	out io.Writer,
	report *conformanceExecutionReport,
) error {
	document := buildConformanceJUnit(report)
	data, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if _, err := io.WriteString(out, xml.Header); err != nil {
		return err
	}
	if _, err := out.Write(data); err != nil {
		return err
	}
	_, err = io.WriteString(out, "\n")
	return err
}

func buildConformanceJUnit(
	report *conformanceExecutionReport,
) conformanceJUnitTestSuites {
	document := conformanceJUnitTestSuites{
		Name: sanitizeConformanceXML(conformanceRunnerName),
		Suites: make(
			[]conformanceJUnitTestSuite,
			0,
			len(report.Targets),
		),
	}

	for _, target := range report.Targets {
		suite := conformanceJUnitTestSuite{
			Name: sanitizeConformanceXML(target.RequestedURL),
			Properties: conformanceJUnitProperties{
				Values: conformanceJUnitSuiteProperties(report, target),
			},
			TestCases: make(
				[]conformanceJUnitTestCase,
				0,
				len(target.Cases),
			),
		}

		for _, testCase := range target.Cases {
			junitCase := conformanceJUnitTestCase{
				Name: sanitizeConformanceXML(testCase.ID),
				Properties: conformanceJUnitProperties{
					Values: conformanceJUnitCaseProperties(testCase),
				},
			}

			diagnostics := formatConformanceDiagnostics(
				testCase.Diagnostics,
			)
			if diagnostics != "" {
				junitCase.SystemOut = sanitizeConformanceXML(diagnostics)
			}

			switch testCase.Outcome {
			case conformanceOutcomeFailed:
				message := "Conformance case failed"
				text := diagnostics
				if text == "" {
					text = message
				}
				junitCase.Failure = &conformanceJUnitFailure{
					Message: sanitizeConformanceXML(message),
					Text:    sanitizeConformanceXML(text),
				}
				suite.Failures++
			case conformanceOutcomeDependencyNotRun:
				message := "Dependency failed"
				if testCase.BlockedByDependency != "" {
					message = fmt.Sprintf(
						"Dependency %s failed",
						testCase.BlockedByDependency,
					)
				}
				junitCase.Skipped = &conformanceJUnitSkipped{
					Message: sanitizeConformanceXML(message),
				}
				suite.Skipped++
			case conformanceOutcomeSkipped:
				junitCase.Skipped = &conformanceJUnitSkipped{
					Message: "Conformance case skipped",
				}
				suite.Skipped++
			}

			suite.TestCases = append(suite.TestCases, junitCase)
		}

		suite.Tests = len(suite.TestCases)
		document.Tests += suite.Tests
		document.Failures += suite.Failures
		document.Skipped += suite.Skipped
		document.Suites = append(document.Suites, suite)
	}

	return document
}

func conformanceJUnitSuiteProperties(
	report *conformanceExecutionReport,
	target conformanceTargetReport,
) []conformanceJUnitProperty {
	detectedSpecVersion := "unknown"
	if target.DetectedSpecVersion != nil {
		detectedSpecVersion = *target.DetectedSpecVersion
	}

	return newConformanceJUnitProperties(
		[2]string{"schema", report.Schema},
		[2]string{"schema-version", report.SchemaVersion},
		[2]string{"runner", report.Runner.Name},
		[2]string{"build-version", report.Runner.BuildVersion},
		[2]string{"commit", report.Runner.Commit},
		[2]string{"profile", report.Profile.ID},
		[2]string{"spec-version", report.Profile.SpecVersion},
		[2]string{"spec-commit", report.Profile.SpecCommit},
		[2]string{"requested-url", target.RequestedURL},
		[2]string{"effective-url", target.EffectiveURL},
		[2]string{"detected-spec-version", detectedSpecVersion},
		[2]string{"profile-match", strconv.FormatBool(target.ProfileMatch)},
		[2]string{"outcome", target.Outcome},
		[2]string{"output", string(report.Invocation.Output)},
		[2]string{
			"allow-mutations",
			strconv.FormatBool(report.Invocation.AllowMutations),
		},
		[2]string{
			"complete-selection",
			strconv.FormatBool(report.Invocation.CompleteSelection),
		},
		[2]string{
			"requested-test-ids",
			strings.Join(report.Invocation.RequestedTestIDs, ","),
		},
		[2]string{
			"resolved-test-ids",
			strings.Join(report.Invocation.ResolvedTestIDs, ","),
		},
	)
}

func conformanceJUnitCaseProperties(
	testCase conformanceCaseReport,
) []conformanceJUnitProperty {
	warnings, skips := countConformanceDiagnostics(testCase.Diagnostics)
	values := newConformanceJUnitProperties(
		[2]string{"display-name", testCase.DisplayName},
		[2]string{"name", testCase.Name},
		[2]string{"description", testCase.Description},
		[2]string{"profile", testCase.Profile},
		[2]string{"mode", string(testCase.Mode)},
		[2]string{"selection", testCase.Selection},
		[2]string{"dependencies", strings.Join(testCase.Dependencies, ",")},
		[2]string{"outcome", testCase.Outcome},
		[2]string{"warnings", strconv.Itoa(warnings)},
		[2]string{"skips", strconv.Itoa(skips)},
	)
	if testCase.BlockedByDependency != "" {
		values = append(values, conformanceJUnitProperty{
			Name: "blocked-by-dependency",
			Value: sanitizeConformanceXML(
				testCase.BlockedByDependency,
			),
		})
	}
	for i, reference := range testCase.SpecReferences {
		values = append(values, conformanceJUnitProperty{
			Name: sanitizeConformanceXML(
				fmt.Sprintf("specification.%d", i+1),
			),
			Value: sanitizeConformanceXML(reference.URL),
		})
	}
	return values
}

func newConformanceJUnitProperties(
	values ...[2]string,
) []conformanceJUnitProperty {
	result := make([]conformanceJUnitProperty, 0, len(values))
	for _, value := range values {
		result = append(result, conformanceJUnitProperty{
			Name:  sanitizeConformanceXML(value[0]),
			Value: sanitizeConformanceXML(value[1]),
		})
	}
	return result
}

func countConformanceDiagnostics(
	diagnostics []conformanceDiagnostic,
) (int, int) {
	warnings := 0
	skips := 0
	for _, diagnostic := range diagnostics {
		switch diagnostic.Kind {
		case "warning":
			warnings++
		case "skip":
			skips++
		}
		childWarnings, childSkips := countConformanceDiagnostics(
			diagnostic.Diagnostics,
		)
		warnings += childWarnings
		skips += childSkips
	}
	return warnings, skips
}

func formatConformanceDiagnostics(
	diagnostics []conformanceDiagnostic,
) string {
	var result strings.Builder
	writeConformanceDiagnostics(&result, diagnostics, "")
	return result.String()
}

func writeConformanceDiagnostics(
	out *strings.Builder,
	diagnostics []conformanceDiagnostic,
	indent string,
) {
	for _, diagnostic := range diagnostics {
		if diagnostic.Kind == "group" {
			fmt.Fprintf(
				out,
				"%sgroup[%s]: %s\n",
				indent,
				diagnostic.Outcome,
				diagnostic.Name,
			)
			writeConformanceDiagnostics(
				out,
				diagnostic.Diagnostics,
				indent+"  ",
			)
			continue
		}

		fmt.Fprintf(
			out,
			"%s%s: %s",
			indent,
			diagnostic.Kind,
			diagnostic.Message,
		)
		if diagnostic.Truncated {
			out.WriteString(" [truncated]")
		}
		out.WriteByte('\n')
	}
}

func sanitizeConformanceXML(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	for _, char := range value {
		if char == '\t' || char == '\n' || char == '\r' ||
			(char >= 0x20 && char <= 0xD7FF) ||
			(char >= 0xE000 && char <= 0xFFFD) ||
			(char >= 0x10000 && char <= 0x10FFFF) {

			result.WriteRune(char)
		} else {
			result.WriteRune('\uFFFD')
		}
	}
	return result.String()
}
