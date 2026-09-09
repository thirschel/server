package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	pathpkg "path"
	"reflect"
	"regexp"
	"strings"

	. "github.com/xregistry/server/common"
)

const (
	coreConformanceProfileID    = "xregistry-core-1.0-rc4"
	coreSpecRepository          = "https://github.com/xregistry/spec"
	coreSpecCommit              = "d2433a8c726ab096303bd943a4fc6691925f7910"
	conformanceSniffResponseKey = "conformance.sniff.response"
)

type ConformanceProfile struct {
	ID             string
	SpecVersion    string
	SpecRepository string
	SpecCommit     string
	SpecBaseURL    string
}

type SpecReference struct {
	Path   string
	Anchor string
	Title  string
	URL    string
}

type CaseMode string

const (
	CaseModeReadOnly CaseMode = "read-only"
	CaseModeMutation CaseMode = "mutation"
)

type ConformanceCase struct {
	ID             string
	FunctionName   string
	Name           string
	Description    string
	ProfileID      string
	Mode           CaseMode
	Test           TestFn
	Dependencies   []string
	SpecReferences []SpecReference
}

type ConformanceSelection struct {
	RequestedIDs []string
	Cases        []*ConformanceCase
	Requested    map[string]bool

	byID map[string]*ConformanceCase
}

func (selection *ConformanceSelection) IsRequested(id string) bool {
	return selection != nil && selection.Requested[id]
}

func newConformanceProfile(
	id string,
	specVersion string,
	specRepository string,
	specCommit string,
) ConformanceProfile {
	specRepository = strings.TrimRight(specRepository, "/")
	return ConformanceProfile{
		ID:             id,
		SpecVersion:    specVersion,
		SpecRepository: specRepository,
		SpecCommit:     specCommit,
		SpecBaseURL:    specRepository + "/blob/" + specCommit + "/",
	}
}

func newSpecReference(
	profile ConformanceProfile,
	path string,
	anchor string,
	title string,
) SpecReference {
	return SpecReference{
		Path:   path,
		Anchor: anchor,
		Title:  title,
		URL:    profile.SpecBaseURL + path + "#" + anchor,
	}
}

var coreConformanceProfile = newConformanceProfile(
	coreConformanceProfileID,
	SPECVERSION,
	coreSpecRepository,
	coreSpecCommit,
)

var ConformanceProfiles = []ConformanceProfile{
	coreConformanceProfile,
}

var ConformanceCatalog = []ConformanceCase{
	{
		ID:           "core.registry-access",
		FunctionName: "TestSniff",
		Name:         "Registry access",
		Description: "Verify that the registry root is reachable, returns JSON, " +
			"and advertises the exact conformance profile version.",
		ProfileID: coreConformanceProfileID,
		Mode:      CaseModeReadOnly,
		Test:      TestSniff,
		SpecReferences: []SpecReference{
			newSpecReference(coreConformanceProfile, "core/spec.md",
				"registry-entity", "Registry Entity"),
			newSpecReference(coreConformanceProfile, "core/http.md",
				"get-", "GET /"),
		},
	},
	{
		ID:           "core.model",
		FunctionName: "TestModel",
		Name:         "Registry model",
		Description:  "Verify that the registry model can be retrieved and parsed.",
		ProfileID:    coreConformanceProfileID,
		Mode:         CaseModeReadOnly,
		Test:         TestModel,
		Dependencies: []string{"core.registry-access"},
		SpecReferences: []SpecReference{
			newSpecReference(coreConformanceProfile, "core/spec.md",
				"registry-model", "Registry Model"),
			newSpecReference(coreConformanceProfile, "core/http.md",
				"get-model", "GET /model"),
		},
	},
	{
		ID:           "core.capabilities",
		FunctionName: "TestCapabilities",
		Name:         "Registry capabilities",
		Description: "Verify that registry capabilities can be retrieved, parsed, " +
			"and projected consistently.",
		ProfileID:    coreConformanceProfileID,
		Mode:         CaseModeReadOnly,
		Test:         TestCapabilities,
		Dependencies: []string{"core.registry-access"},
		SpecReferences: []SpecReference{
			newSpecReference(coreConformanceProfile, "core/spec.md",
				"registry-capabilities", "Registry Capabilities"),
			newSpecReference(coreConformanceProfile, "core/http.md",
				"get-capabilities", "GET /capabilities"),
		},
	},
	{
		ID:           "core.registry-root",
		FunctionName: "TestRegistryRoot",
		Name:         "Registry root",
		Description:  "Verify the required registry root metadata and collections.",
		ProfileID:    coreConformanceProfileID,
		Mode:         CaseModeReadOnly,
		Test:         TestRegistryRoot,
		Dependencies: []string{"core.model", "core.capabilities"},
		SpecReferences: []SpecReference{
			newSpecReference(coreConformanceProfile, "core/spec.md",
				"registry-entity", "Registry Entity"),
			newSpecReference(coreConformanceProfile, "core/http.md",
				"get-", "GET /"),
		},
	},
	{
		ID:           "core.groups",
		FunctionName: "TestGroups",
		Name:         "Groups",
		Description:  "Verify advertised group collections and group entities.",
		ProfileID:    coreConformanceProfileID,
		Mode:         CaseModeReadOnly,
		Test:         TestGroups,
		Dependencies: []string{"core.registry-root"},
		SpecReferences: []SpecReference{
			newSpecReference(coreConformanceProfile, "core/spec.md",
				"group-entity", "Group Entity"),
			newSpecReference(coreConformanceProfile, "core/http.md",
				"group-entity", "Group Entity HTTP APIs"),
		},
	},
	{
		ID:           "core.resources",
		FunctionName: "TestResources",
		Name:         "Resources",
		Description:  "Verify advertised resource collections and resource entities.",
		ProfileID:    coreConformanceProfileID,
		Mode:         CaseModeReadOnly,
		Test:         TestResources,
		Dependencies: []string{"core.groups"},
		SpecReferences: []SpecReference{
			newSpecReference(coreConformanceProfile, "core/spec.md",
				"resource-entity", "Resource Entity"),
			newSpecReference(coreConformanceProfile, "core/http.md",
				"resource-entity", "Resource Entity HTTP APIs"),
		},
	},
}

var (
	caseIDPattern     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*(?:\.[a-z0-9]+(?:-[a-z0-9]+)*)+$`)
	profileIDPattern  = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	specCommitPattern = regexp.MustCompile(
		`^[0-9a-f]{40}$`,
	)
	specAnchorPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

func init() {
	if err := validateConformanceCatalog(
		ConformanceProfiles,
		ConformanceCatalog,
	); err != nil {
		panic(err)
	}
}

func validateConformanceCatalog(
	profiles []ConformanceProfile,
	catalog []ConformanceCase,
) error {
	profileByID := map[string]ConformanceProfile{}
	for i, profile := range profiles {
		if !profileIDPattern.MatchString(profile.ID) {
			return fmt.Errorf("conformance profile %d has invalid ID %q",
				i, profile.ID)
		}
		if _, ok := profileByID[profile.ID]; ok {
			return fmt.Errorf("duplicate conformance profile ID %q", profile.ID)
		}
		if profile.SpecVersion != SPECVERSION {
			return fmt.Errorf(
				"conformance profile %q version %q does not match %q",
				profile.ID, profile.SpecVersion, SPECVERSION)
		}
		if err := validateConformanceProfile(profile); err != nil {
			return err
		}
		profileByID[profile.ID] = profile
	}

	caseByID := map[string]*ConformanceCase{}
	functions := map[uintptr]string{}
	for i := range catalog {
		testCase := &catalog[i]
		if !caseIDPattern.MatchString(testCase.ID) {
			return fmt.Errorf("conformance case %d has invalid ID %q",
				i, testCase.ID)
		}
		if _, ok := caseByID[testCase.ID]; ok {
			return fmt.Errorf("duplicate conformance case ID %q", testCase.ID)
		}
		if testCase.Test == nil {
			return fmt.Errorf("conformance case %q has a nil test function",
				testCase.ID)
		}

		functionPointer := reflect.ValueOf(testCase.Test).Pointer()
		if existingID, ok := functions[functionPointer]; ok {
			return fmt.Errorf(
				"conformance cases %q and %q register the same test function",
				existingID, testCase.ID)
		}
		functions[functionPointer] = testCase.ID

		if testCase.FunctionName != testCase.Test.DisplayName() {
			return fmt.Errorf(
				"conformance case %q function name %q does not match %q",
				testCase.ID, testCase.FunctionName,
				testCase.Test.DisplayName())
		}
		if strings.TrimSpace(testCase.Name) == "" {
			return fmt.Errorf("conformance case %q has an empty name", testCase.ID)
		}
		if strings.TrimSpace(testCase.Description) == "" {
			return fmt.Errorf("conformance case %q has an empty description",
				testCase.ID)
		}
		profile, ok := profileByID[testCase.ProfileID]
		if !ok {
			return fmt.Errorf("conformance case %q references unknown profile %q",
				testCase.ID, testCase.ProfileID)
		}
		if testCase.Mode != CaseModeReadOnly &&
			testCase.Mode != CaseModeMutation {

			return fmt.Errorf("conformance case %q has invalid mode %q",
				testCase.ID, testCase.Mode)
		}
		if len(testCase.SpecReferences) == 0 {
			return fmt.Errorf("conformance case %q has no specification references",
				testCase.ID)
		}
		for j, ref := range testCase.SpecReferences {
			if err := validateSpecReference(profile, ref); err != nil {
				return fmt.Errorf(
					"conformance case %q specification reference %d: %w",
					testCase.ID, j, err)
			}
		}

		caseByID[testCase.ID] = testCase
	}

	for i := range catalog {
		testCase := &catalog[i]
		dependencies := map[string]bool{}
		for _, dependencyID := range testCase.Dependencies {
			if dependencies[dependencyID] {
				return fmt.Errorf(
					"conformance case %q has duplicate dependency %q",
					testCase.ID, dependencyID)
			}
			dependencies[dependencyID] = true

			dependency, ok := caseByID[dependencyID]
			if !ok {
				return fmt.Errorf(
					"conformance case %q references unknown dependency %q",
					testCase.ID, dependencyID)
			}
			if testCase.Mode == CaseModeReadOnly &&
				dependency.Mode == CaseModeMutation {

				return fmt.Errorf(
					"read-only conformance case %q depends on mutation case %q",
					testCase.ID, dependencyID)
			}
		}
	}

	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("conformance catalog dependency cycle includes %q",
				id)
		}
		if visited[id] {
			return nil
		}

		visiting[id] = true
		for _, dependencyID := range caseByID[id].Dependencies {
			if err := visit(dependencyID); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	for i := range catalog {
		if err := visit(catalog[i].ID); err != nil {
			return err
		}
	}

	position := map[string]int{}
	for i := range catalog {
		position[catalog[i].ID] = i
	}
	for i := range catalog {
		for _, dependencyID := range catalog[i].Dependencies {
			if position[dependencyID] >= i {
				return fmt.Errorf(
					"conformance case %q appears before dependency %q",
					catalog[i].ID, dependencyID)
			}
		}
	}

	return nil
}

func validateConformanceProfile(profile ConformanceProfile) error {
	repositoryURL, err := url.Parse(profile.SpecRepository)
	if err != nil || repositoryURL.Scheme != "https" ||
		repositoryURL.Host == "" || repositoryURL.RawQuery != "" ||
		repositoryURL.Fragment != "" ||
		strings.HasSuffix(profile.SpecRepository, "/") {

		return fmt.Errorf(
			"conformance profile %q has invalid specification repository %q",
			profile.ID, profile.SpecRepository)
	}
	if !specCommitPattern.MatchString(profile.SpecCommit) {
		return fmt.Errorf(
			"conformance profile %q has invalid specification commit %q",
			profile.ID, profile.SpecCommit)
	}

	expectedBaseURL := profile.SpecRepository + "/blob/" +
		profile.SpecCommit + "/"
	if profile.SpecBaseURL != expectedBaseURL {
		return fmt.Errorf(
			"conformance profile %q specification URL must be immutable: got %q, want %q",
			profile.ID, profile.SpecBaseURL, expectedBaseURL)
	}
	return nil
}

func validateSpecReference(
	profile ConformanceProfile,
	ref SpecReference,
) error {
	cleanPath := pathpkg.Clean(ref.Path)
	if ref.Path == "" || cleanPath == "." || cleanPath != ref.Path ||
		strings.HasPrefix(ref.Path, "/") ||
		strings.Contains(ref.Path, `\`) ||
		strings.HasPrefix(ref.Path, "../") ||
		!strings.HasSuffix(ref.Path, ".md") {

		return fmt.Errorf("invalid repository-relative path %q", ref.Path)
	}
	if !specAnchorPattern.MatchString(ref.Anchor) {
		return fmt.Errorf("invalid Markdown anchor %q", ref.Anchor)
	}
	if strings.TrimSpace(ref.Title) == "" {
		return fmt.Errorf("empty display title")
	}

	expectedURL := profile.SpecBaseURL + ref.Path + "#" + ref.Anchor
	parsedURL, err := url.Parse(ref.URL)
	if err != nil || !parsedURL.IsAbs() || parsedURL.Fragment != ref.Anchor {
		return fmt.Errorf("invalid specification URL %q", ref.URL)
	}
	if ref.URL != expectedURL {
		return fmt.Errorf(
			"specification URL must be immutable: got %q, want %q",
			ref.URL, expectedURL)
	}
	return nil
}

func resolveConformanceSelection(
	catalog []ConformanceCase,
	requestedIDs []string,
	allowMutations bool,
) (*ConformanceSelection, error) {
	caseByID := map[string]*ConformanceCase{}
	validIDs := make([]string, 0, len(catalog))
	for i := range catalog {
		testCase := &catalog[i]
		caseByID[testCase.ID] = testCase
		validIDs = append(validIDs, testCase.ID)
	}

	requested := map[string]bool{}
	deduplicatedIDs := make([]string, 0, len(requestedIDs))
	if len(requestedIDs) == 0 {
		for _, id := range validIDs {
			requested[id] = true
			deduplicatedIDs = append(deduplicatedIDs, id)
		}
	} else {
		for _, id := range requestedIDs {
			if _, ok := caseByID[id]; !ok {
				return nil, fmt.Errorf(
					"unknown conformance test ID %q; valid IDs: %s",
					id, strings.Join(validIDs, ", "))
			}
			if requested[id] {
				continue
			}
			requested[id] = true
			deduplicatedIDs = append(deduplicatedIDs, id)
		}
	}

	selected := map[string]bool{}
	var include func(string)
	include = func(id string) {
		if selected[id] {
			return
		}
		selected[id] = true
		for _, dependencyID := range caseByID[id].Dependencies {
			include(dependencyID)
		}
	}
	for _, id := range deduplicatedIDs {
		include(id)
	}

	selection := &ConformanceSelection{
		RequestedIDs: deduplicatedIDs,
		Requested:    requested,
		byID:         caseByID,
	}
	for i := range catalog {
		testCase := &catalog[i]
		if !selected[testCase.ID] {
			continue
		}
		if testCase.Mode == CaseModeMutation && !allowMutations {
			return nil, fmt.Errorf(
				"conformance test %q requires --allow-mutations",
				testCase.ID)
		}
		selection.Cases = append(selection.Cases, testCase)
	}

	return selection, nil
}

func renderConformanceCatalog(
	out io.Writer,
	profiles []ConformanceProfile,
	catalog []ConformanceCase,
) {
	fmt.Fprintln(out, "xRegistry conformance catalog")
	for _, profile := range profiles {
		fmt.Fprintf(out, "\nProfile: %s\n", profile.ID)
		fmt.Fprintf(out, "  spec version: %s\n", profile.SpecVersion)
		fmt.Fprintf(out, "  spec repository: %s\n", profile.SpecRepository)
		fmt.Fprintf(out, "  spec commit: %s\n", profile.SpecCommit)
		fmt.Fprintf(out, "  spec base URL: %s\n", profile.SpecBaseURL)
	}

	fmt.Fprintln(out, "\nTests:")
	for _, testCase := range catalog {
		fmt.Fprintf(out, "\n%s (%s)\n", testCase.ID, testCase.FunctionName)
		fmt.Fprintf(out, "  name: %s\n", testCase.Name)
		fmt.Fprintf(out, "  description: %s\n", testCase.Description)
		fmt.Fprintf(out, "  mode: %s\n", testCase.Mode)
		fmt.Fprintf(out, "  profile: %s\n", testCase.ProfileID)
		if len(testCase.Dependencies) == 0 {
			fmt.Fprintln(out, "  dependencies: none")
		} else {
			fmt.Fprintf(out, "  dependencies: %s\n",
				strings.Join(testCase.Dependencies, ", "))
		}
		fmt.Fprintln(out, "  specification:")
		for _, ref := range testCase.SpecReferences {
			fmt.Fprintf(out, "    - %s: %s\n", ref.Title, ref.URL)
		}
	}
}

func validateConformanceRequest(
	testCase *ConformanceCase,
	allowMutations bool,
	method string,
) error {
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}

	if testCase == nil {
		return fmt.Errorf(
			"conformance request guard blocked %s without an active test",
			method)
	}
	if testCase.Mode == CaseModeMutation && allowMutations {
		return nil
	}
	if testCase.Mode == CaseModeMutation {
		return fmt.Errorf(
			"conformance test %q requires --allow-mutations to send %s requests",
			testCase.ID, method)
	}
	return fmt.Errorf(
		"read-only conformance test %q may not send %s requests",
		testCase.ID, method)
}
