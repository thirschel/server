package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/xregistry/server/cmds/xr/xrlib"
)

const expectedConformanceCatalogOutput = `xRegistry conformance catalog

Profile: xregistry-core-1.0-rc4
  spec version: 1.0-rc4
  spec repository: https://github.com/xregistry/spec
  spec commit: d2433a8c726ab096303bd943a4fc6691925f7910
  spec base URL: https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/

Tests:

core.registry-access (TestSniff)
  name: Registry access
  description: Verify that the registry root is reachable, returns JSON, and advertises the exact conformance profile version.
  mode: read-only
  profile: xregistry-core-1.0-rc4
  dependencies: none
  specification:
    - Registry Entity: https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/spec.md#registry-entity
    - GET /: https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/http.md#get-

core.model (TestModel)
  name: Registry model
  description: Verify that the registry model can be retrieved and parsed.
  mode: read-only
  profile: xregistry-core-1.0-rc4
  dependencies: core.registry-access
  specification:
    - Registry Model: https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/spec.md#registry-model
    - GET /model: https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/http.md#get-model

core.capabilities (TestCapabilities)
  name: Registry capabilities
  description: Verify that registry capabilities can be retrieved, parsed, and projected consistently.
  mode: read-only
  profile: xregistry-core-1.0-rc4
  dependencies: core.registry-access
  specification:
    - Registry Capabilities: https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/spec.md#registry-capabilities
    - GET /capabilities: https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/http.md#get-capabilities

core.registry-root (TestRegistryRoot)
  name: Registry root
  description: Verify the required registry root metadata and collections.
  mode: read-only
  profile: xregistry-core-1.0-rc4
  dependencies: core.model, core.capabilities
  specification:
    - Registry Entity: https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/spec.md#registry-entity
    - GET /: https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/http.md#get-

core.groups (TestGroups)
  name: Groups
  description: Verify advertised group collections and group entities.
  mode: read-only
  profile: xregistry-core-1.0-rc4
  dependencies: core.registry-root
  specification:
    - Group Entity: https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/spec.md#group-entity
    - Group Entity HTTP APIs: https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/http.md#group-entity

core.resources (TestResources)
  name: Resources
  description: Verify advertised resource collections and resource entities.
  mode: read-only
  profile: xregistry-core-1.0-rc4
  dependencies: core.groups
  specification:
    - Resource Entity: https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/spec.md#resource-entity
    - Resource Entity HTTP APIs: https://github.com/xregistry/spec/blob/d2433a8c726ab096303bd943a4fc6691925f7910/core/http.md#resource-entity
`

func TestConformanceCatalogValidation(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		if err := validateConformanceCatalog(
			copyConformanceProfiles(),
			copyConformanceCatalog(),
		); err != nil {
			t.Fatal(err)
		}
	})

	tests := []struct {
		name   string
		change func([]ConformanceProfile, []ConformanceCase)
		want   string
	}{
		{
			name: "duplicate IDs",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[1].ID = catalog[0].ID
			},
			want: "duplicate conformance case ID",
		},
		{
			name: "duplicate functions",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[1].Test = catalog[0].Test
				catalog[1].FunctionName = catalog[0].FunctionName
			},
			want: "register the same test function",
		},
		{
			name: "nil function",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[0].Test = nil
			},
			want: "nil test function",
		},
		{
			name: "unknown profile",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[0].ProfileID = "unknown-profile"
			},
			want: "unknown profile",
		},
		{
			name: "invalid mode",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[0].Mode = "unsafe"
			},
			want: "invalid mode",
		},
		{
			name: "empty spec reference",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[0].SpecReferences = nil
			},
			want: "no specification references",
		},
		{
			name: "malformed spec path",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[0].SpecReferences[0].Path = "../spec.md"
			},
			want: "invalid repository-relative path",
		},
		{
			name: "malformed spec anchor",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[0].SpecReferences[0].Anchor = "#registry"
			},
			want: "invalid Markdown anchor",
		},
		{
			name: "empty spec title",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[0].SpecReferences[0].Title = ""
			},
			want: "empty display title",
		},
		{
			name: "mutable profile URL",
			change: func(profiles []ConformanceProfile, _ []ConformanceCase) {
				profiles[0].SpecBaseURL =
					profiles[0].SpecRepository + "/blob/main/"
			},
			want: "specification URL must be immutable",
		},
		{
			name: "mutable reference URL",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[0].SpecReferences[0].URL =
					coreSpecRepository + "/blob/main/core/spec.md#registry-entity"
			},
			want: "specification URL must be immutable",
		},
		{
			name: "duplicate dependencies",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[1].Dependencies = []string{
					"core.registry-access",
					"core.registry-access",
				}
			},
			want: "duplicate dependency",
		},
		{
			name: "unknown dependency",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[1].Dependencies = []string{"core.unknown"}
			},
			want: "unknown dependency",
		},
		{
			name: "dependency cycle",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[0].Dependencies = []string{"core.model"}
			},
			want: "dependency cycle",
		},
		{
			name: "dependency order",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[0], catalog[1] = catalog[1], catalog[0]
			},
			want: "appears before dependency",
		},
		{
			name: "read-only dependency on mutation",
			change: func(_ []ConformanceProfile, catalog []ConformanceCase) {
				catalog[0].Mode = CaseModeMutation
			},
			want: "depends on mutation case",
		},
		{
			name: "profile version drift",
			change: func(profiles []ConformanceProfile, _ []ConformanceCase) {
				profiles[0].SpecVersion = "1.0-rc5"
			},
			want: "does not match",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profiles := copyConformanceProfiles()
			catalog := copyConformanceCatalog()
			test.change(profiles, catalog)

			err := validateConformanceCatalog(profiles, catalog)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestConformanceProfileAndSpecReferencesArePinned(t *testing.T) {
	if len(ConformanceProfiles) != 1 {
		t.Fatalf("Got %d profiles, want 1", len(ConformanceProfiles))
	}
	profile := ConformanceProfiles[0]
	if profile.ID != coreConformanceProfileID {
		t.Fatalf("Profile ID %q, want %q", profile.ID,
			coreConformanceProfileID)
	}
	if profile.SpecVersion != "1.0-rc4" {
		t.Fatalf("Spec version %q, want 1.0-rc4", profile.SpecVersion)
	}
	if profile.SpecCommit != coreSpecCommit {
		t.Fatalf("Spec commit %q, want %q", profile.SpecCommit, coreSpecCommit)
	}

	expectedBaseURL := coreSpecRepository + "/blob/" + coreSpecCommit + "/"
	if profile.SpecBaseURL != expectedBaseURL {
		t.Fatalf("Spec base URL %q, want %q",
			profile.SpecBaseURL, expectedBaseURL)
	}
	for _, testCase := range ConformanceCatalog {
		if testCase.Mode != CaseModeReadOnly {
			t.Fatalf("Initial case %q is not read-only", testCase.ID)
		}
		for _, ref := range testCase.SpecReferences {
			if !strings.HasPrefix(ref.URL, expectedBaseURL) {
				t.Fatalf("Case %q has unpinned reference %q",
					testCase.ID, ref.URL)
			}
			if strings.Contains(ref.URL, "/blob/main/") ||
				strings.Contains(ref.URL, "/blob/master/") {

				t.Fatalf("Case %q has mutable reference %q",
					testCase.ID, ref.URL)
			}
		}
	}
}

func TestConformanceCatalogListingIsExact(t *testing.T) {
	first := bytes.Buffer{}
	renderConformanceCatalog(
		&first,
		ConformanceProfiles,
		ConformanceCatalog,
	)
	second := bytes.Buffer{}
	renderConformanceCatalog(
		&second,
		ConformanceProfiles,
		ConformanceCatalog,
	)

	if first.String() != expectedConformanceCatalogOutput {
		t.Fatalf("Unexpected catalog output: %s",
			Diff(expectedConformanceCatalogOutput, first.String()))
	}
	if first.String() != second.String() {
		t.Fatalf("Catalog output is not deterministic: %s",
			Diff(first.String(), second.String()))
	}
}

func TestListTestsIsOffline(t *testing.T) {
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

	output := captureTestStdout(t, func() {
		conformFunc(cmd, nil)
	})
	if output != expectedConformanceCatalogOutput {
		t.Fatalf("Unexpected --list-tests output: %s",
			Diff(expectedConformanceCatalogOutput, output))
	}
	if requests.Total() != 0 {
		t.Fatalf("--list-tests sent %d requests", requests.Total())
	}
}

func TestConformanceSelection(t *testing.T) {
	selection, err := resolveConformanceSelection(
		ConformanceCatalog,
		[]string{
			"core.resources",
			"core.model",
			"core.resources",
		},
		false,
	)
	if err != nil {
		t.Fatal(err)
	}

	assertStringsEqual(t, selection.RequestedIDs, []string{
		"core.resources",
		"core.model",
	})
	assertSelectionIDs(t, selection, []string{
		"core.registry-access",
		"core.model",
		"core.capabilities",
		"core.registry-root",
		"core.groups",
		"core.resources",
	})
	if !selection.IsRequested("core.resources") ||
		!selection.IsRequested("core.model") {

		t.Fatal("Requested cases were not tracked")
	}
	for _, dependencyID := range []string{
		"core.registry-access",
		"core.capabilities",
		"core.registry-root",
		"core.groups",
	} {
		if selection.IsRequested(dependencyID) {
			t.Fatalf("Dependency-only case %q was marked requested",
				dependencyID)
		}
	}

	capabilities, err := resolveConformanceSelection(
		ConformanceCatalog,
		[]string{"core.capabilities"},
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertSelectionIDs(t, capabilities, []string{
		"core.registry-access",
		"core.capabilities",
	})
}

func TestUnknownConformanceSelectionIsCaseSensitiveAndOffline(t *testing.T) {
	server, requests := newObservedConformanceServer(
		t,
		`"specversion": "1.0-rc4"`,
	)
	options := testConformOptions()
	options.testIDs = []string{"Core.model"}

	out := bytes.Buffer{}
	rc, err := runConform([]string{server.URL}, &out, options)
	if err == nil ||
		!strings.Contains(err.Error(), `unknown conformance test ID "Core.model"`) ||
		!strings.Contains(err.Error(), "core.registry-access") {

		t.Fatalf("Unexpected selection error: %v", err)
	}
	if rc != 0 {
		t.Fatalf("Selection error returned rc %d, want 0", rc)
	}
	if out.Len() != 0 {
		t.Fatalf("Selection error wrote target output: %q", out.String())
	}
	if requests.Total() != 0 {
		t.Fatalf("Unknown selection sent %d requests", requests.Total())
	}
}

func TestSelectedCasesRunInCatalogOrderWithDeclaredClosure(t *testing.T) {
	for _, requestedCase := range ConformanceCatalog {
		t.Run(requestedCase.ID, func(t *testing.T) {
			server, _ := newObservedConformanceServer(
				t,
				`"specversion": "1.0-rc4"`,
			)
			options := testConformOptions()
			options.testIDs = []string{requestedCase.ID}

			out, rc := testConformOutput([]string{server.URL}, options)
			if rc != 0 {
				t.Fatalf("Selected run failed (%d):\n%s", rc, out)
			}

			selection, err := resolveConformanceSelection(
				ConformanceCatalog,
				options.testIDs,
				false,
			)
			if err != nil {
				t.Fatal(err)
			}
			selected := map[string]bool{}
			lastPosition := -1
			for _, testCase := range selection.Cases {
				selected[testCase.FunctionName] = true
				header := "PASS: " + testCase.FunctionName
				if strings.Count(out, header) != 1 {
					t.Fatalf("Expected one %s header:\n%s",
						testCase.FunctionName, out)
				}
				position := strings.Index(out, header)
				if position <= lastPosition {
					t.Fatalf("Cases did not run in catalog order:\n%s", out)
				}
				lastPosition = position
			}
			for _, testCase := range ConformanceCatalog {
				if selected[testCase.FunctionName] {
					continue
				}
				if strings.Contains(
					out,
					"PASS: "+testCase.FunctionName,
				) {
					t.Fatalf("Undeclared case %s ran:\n%s",
						testCase.FunctionName, out)
				}
			}
		})
	}

	server, _ := newObservedConformanceServer(
		t,
		`"specversion": "1.0-rc4"`,
	)
	options := testConformOptions()
	options.testIDs = []string{"core.resources", "core.model"}
	out, rc := testConformOutput([]string{server.URL}, options)
	if rc != 0 {
		t.Fatalf("Reordered selection failed (%d):\n%s", rc, out)
	}
	if strings.Index(out, "PASS: TestModel") >
		strings.Index(out, "PASS: TestResources") {

		t.Fatalf("Command-line order changed catalog execution order:\n%s", out)
	}
}

func TestExactRC4ProfileAndRequestCompatibility(t *testing.T) {
	server, requests := newObservedConformanceServer(
		t,
		`"specversion": "1.0-rc4"`,
	)
	out, rc := testConformOutput(
		[]string{server.URL},
		testConformOptions(),
	)
	if rc != 0 {
		t.Fatalf("Exact rc4 run failed (%d):\n%s", rc, out)
	}

	for path, want := range map[string]int{
		"/":             3,
		"/model":        2,
		"/capabilities": 2,
	} {
		if got := requests.Count(path); got != want {
			t.Fatalf("Path %s received %d requests, want %d", path, got, want)
		}
	}
	for _, request := range requests.All() {
		if request.method != http.MethodGet {
			t.Fatalf("Catalog sent %s %s, want GET",
				request.method, request.path)
		}
	}
}

func TestUnsupportedProfilesFailBeforeDependentRequests(t *testing.T) {
	tests := []struct {
		name      string
		specField string
	}{
		{name: "missing"},
		{name: "null", specField: `"specversion": null`},
		{name: "non-string", specField: `"specversion": 4`},
		{name: "rc3", specField: `"specversion": "1.0-rc3"`},
		{name: "rc5", specField: `"specversion": "1.0-rc5"`},
		{name: "1.0", specField: `"specversion": "1.0"`},
		{name: "1.1", specField: `"specversion": "1.1"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, requests := newObservedConformanceServer(
				t,
				test.specField,
			)
			out := bytes.Buffer{}
			rc, err := runConform(
				[]string{server.URL},
				&out,
				testConformOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if rc == 0 {
				t.Fatalf("Unsupported profile passed:\n%s", out.String())
			}
			if requests.Total() != 1 || requests.Count("/") != 1 {
				t.Fatalf("Unsupported profile sent requests after sniff: %#v",
					requests.All())
			}
			if !strings.Contains(out.String(), "FAIL: TestSniff") {
				t.Fatalf("Missing sniff failure:\n%s", out.String())
			}
		})
	}
}

func TestSniffStoresProfileResponse(t *testing.T) {
	server, _ := newObservedConformanceServer(
		t,
		`"specversion": "1.0-rc4"`,
	)
	reg := xrlib.DefineRegistry(server.URL)
	td := NewTD(nil, server.URL)
	td.SetRegistry(reg)

	TDClear()
	defer TDClear()
	caseTD := td.RunCase(&ConformanceCatalog[0])
	if caseTD.Status != PASS {
		t.Fatalf("Sniff failed: %#v", caseTD)
	}

	value, ok := reg.GetStuff(conformanceSniffResponseKey)
	if !ok {
		t.Fatal("Sniff response was not stored on the registry")
	}
	response, ok := value.(*xrlib.HttpResponse)
	if !ok || response == nil {
		t.Fatalf("Stored sniff response has type %T", value)
	}
	if response.JSON["specversion"] != "1.0-rc4" {
		t.Fatalf("Stored specversion is %#v",
			response.JSON["specversion"])
	}
}

func TestMutationSelectionAndTransportGuard(t *testing.T) {
	t.Run("selection requires permission before network", func(t *testing.T) {
		server, requests := newMethodProbeServer(t)
		catalog := []ConformanceCase{
			syntheticRequestCase(CaseModeMutation),
		}
		options := testConformOptions()
		options.testIDs = []string{catalog[0].ID}

		out := bytes.Buffer{}
		rc, err := runConformWithCatalog(
			[]string{server.URL},
			&out,
			options,
			copyConformanceProfiles(),
			catalog,
		)
		if err == nil ||
			!strings.Contains(err.Error(), "--allow-mutations") {

			t.Fatalf("Unexpected mutation selection error: %v", err)
		}
		if rc != 0 || out.Len() != 0 || requests.Total() != 0 {
			t.Fatalf(
				"Mutation selection reached execution: rc=%d out=%q requests=%d",
				rc, out.String(), requests.Total())
		}
	})

	t.Run("mutation dependency requires permission before network", func(t *testing.T) {
		server, requests := newMethodProbeServer(t)
		dependency := syntheticRequestCase(CaseModeMutation)
		dependency.ID = "core.method-dependency"
		parentFn := TestFn(conformanceNoopProbe)
		parent := ConformanceCase{
			ID:           "core.method-parent",
			FunctionName: parentFn.DisplayName(),
			Name:         "Method parent",
			Description:  "Select a synthetic mutation dependency.",
			ProfileID:    coreConformanceProfileID,
			Mode:         CaseModeMutation,
			Test:         parentFn,
			Dependencies: []string{dependency.ID},
			SpecReferences: []SpecReference{
				newSpecReference(coreConformanceProfile, "core/http.md",
					"registry-http-apis", "Registry HTTP APIs"),
			},
		}
		options := testConformOptions()
		options.testIDs = []string{parent.ID}

		out := bytes.Buffer{}
		rc, err := runConformWithCatalog(
			[]string{server.URL},
			&out,
			options,
			copyConformanceProfiles(),
			[]ConformanceCase{dependency, parent},
		)
		if err == nil ||
			!strings.Contains(err.Error(), dependency.ID) ||
			!strings.Contains(err.Error(), "--allow-mutations") {

			t.Fatalf("Unexpected mutation dependency error: %v", err)
		}
		if rc != 0 || out.Len() != 0 || requests.Total() != 0 {
			t.Fatalf(
				"Mutation dependency reached execution: rc=%d out=%q requests=%d",
				rc, out.String(), requests.Total())
		}
	})

	t.Run("read-only case is blocked even with permission", func(t *testing.T) {
		server, requests := newMethodProbeServer(t)
		catalog := []ConformanceCase{
			syntheticRequestCase(CaseModeReadOnly),
		}
		options := testConformOptions()
		options.testIDs = []string{catalog[0].ID}
		options.allowMutations = true
		options.wrapAt = 0

		out := bytes.Buffer{}
		rc, err := runConformWithCatalog(
			[]string{server.URL},
			&out,
			options,
			copyConformanceProfiles(),
			catalog,
		)
		if err != nil {
			t.Fatal(err)
		}
		if rc == 0 {
			t.Fatalf("Read-only mutation unexpectedly passed:\n%s",
				out.String())
		}
		if requests.Total() != 0 {
			t.Fatalf("Blocked request reached the server %d times",
				requests.Total())
		}
		if !strings.Contains(
			out.String(),
			`read-only conformance test "core.method-probe" may not send POST requests`,
		) {
			t.Fatalf("Missing request guard diagnostic:\n%s", out.String())
		}
	})

	t.Run("mutation case is allowed with permission", func(t *testing.T) {
		server, requests := newMethodProbeServer(t)
		catalog := []ConformanceCase{
			syntheticRequestCase(CaseModeMutation),
		}
		options := testConformOptions()
		options.testIDs = []string{catalog[0].ID}
		options.allowMutations = true

		out := bytes.Buffer{}
		rc, err := runConformWithCatalog(
			[]string{server.URL},
			&out,
			options,
			copyConformanceProfiles(),
			catalog,
		)
		if err != nil {
			t.Fatal(err)
		}
		if rc != 0 {
			t.Fatalf("Permitted mutation failed (%d):\n%s", rc, out.String())
		}
		if requests.Total() != 1 ||
			requests.All()[0].method != http.MethodPost {

			t.Fatalf("Mutation requests: %#v", requests.All())
		}
	})

	t.Run("safe methods are always permitted", func(t *testing.T) {
		testCase := ConformanceCatalog[0]
		for _, method := range []string{
			http.MethodGet,
			http.MethodHead,
			http.MethodOptions,
		} {
			if err := validateConformanceRequest(
				&testCase,
				false,
				method,
			); err != nil {
				t.Fatalf("%s was blocked: %v", method, err)
			}
		}
	})
}

func TestRegistryRequestGuardRunsBeforeTransport(t *testing.T) {
	server, requests := newMethodProbeServer(t)
	reg := xrlib.DefineRegistry(server.URL)

	var gotMethod string
	var gotURL string
	reg.SetRequestGuard(func(method string, requestURL string) error {
		gotMethod = method
		gotURL = requestURL
		return errors.New("blocked by test guard")
	})

	response, xErr := reg.HttpDo(false, http.MethodDelete, "/blocked", nil)
	if xErr == nil ||
		!strings.Contains(xErr.GetTitle(), "blocked by test guard") {

		t.Fatalf("Unexpected guard error: %v", xErr)
	}
	if response == nil || response.Error != xErr {
		t.Fatalf("Guard response did not preserve the error: %#v", response)
	}
	if gotMethod != http.MethodDelete ||
		gotURL != server.URL+"/blocked" {

		t.Fatalf("Guard saw %s %s", gotMethod, gotURL)
	}
	if requests.Total() != 0 {
		t.Fatalf("Guarded request reached the server %d times",
			requests.Total())
	}
}

func TestConformanceInvocationFlagCompatibility(t *testing.T) {
	tests := []struct {
		name      string
		listTests bool
		args      []string
		flags     map[string]string
		want      string
	}{
		{
			name:      "list only",
			listTests: true,
		},
		{
			name:      "list positional URL",
			listTests: true,
			args:      []string{"https://example.com"},
			want:      "does not accept target URLs",
		},
		{
			name:      "list execution flag",
			listTests: true,
			flags:     map[string]string{"depth": "1"},
			want:      "cannot be combined with --depth",
		},
		{
			name: "run and list",
			flags: map[string]string{
				"run":        "TestTDAllPass",
				"list-tests": "true",
			},
			want: "cannot be combined with --list-tests",
		},
		{
			name: "run and test",
			flags: map[string]string{
				"run":  "TestTDAllPass",
				"test": "core.model",
			},
			want: "cannot be combined with --test",
		},
		{
			name: "run and mutation permission",
			flags: map[string]string{
				"run":             "TestTDAllPass",
				"allow-mutations": "true",
			},
			want: "cannot be combined with --allow-mutations",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := newConformanceInvocationTestCommand()
			for name, value := range test.flags {
				if err := cmd.Flags().Set(name, value); err != nil {
					t.Fatal(err)
				}
			}

			err := validateConformInvocation(
				cmd,
				test.args,
				test.listTests ||
					cmd.Flags().Changed("list-tests"),
			)
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Expected error containing %q, got %v",
					test.want, err)
			}
		})
	}
}

func copyConformanceProfiles() []ConformanceProfile {
	return append([]ConformanceProfile(nil), ConformanceProfiles...)
}

func copyConformanceCatalog() []ConformanceCase {
	catalog := append([]ConformanceCase(nil), ConformanceCatalog...)
	for i := range catalog {
		catalog[i].Dependencies = append(
			[]string(nil),
			catalog[i].Dependencies...,
		)
		catalog[i].SpecReferences = append(
			[]SpecReference(nil),
			catalog[i].SpecReferences...,
		)
	}
	return catalog
}

func assertSelectionIDs(
	t *testing.T,
	selection *ConformanceSelection,
	want []string,
) {
	t.Helper()
	got := make([]string, 0, len(selection.Cases))
	for _, testCase := range selection.Cases {
		got = append(got, testCase.ID)
	}
	assertStringsEqual(t, got, want)
}

func assertStringsEqual(t *testing.T, got []string, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Got %#v, want %#v", got, want)
	}
}

type observedRequest struct {
	method string
	path   string
}

type observedRequests struct {
	mu       sync.Mutex
	requests []observedRequest
}

func (requests *observedRequests) Add(method string, path string) {
	requests.mu.Lock()
	defer requests.mu.Unlock()
	requests.requests = append(requests.requests, observedRequest{
		method: method,
		path:   path,
	})
}

func (requests *observedRequests) All() []observedRequest {
	requests.mu.Lock()
	defer requests.mu.Unlock()
	return append([]observedRequest(nil), requests.requests...)
}

func (requests *observedRequests) Count(path string) int {
	count := 0
	for _, request := range requests.All() {
		if request.path == path {
			count++
		}
	}
	return count
}

func (requests *observedRequests) Total() int {
	return len(requests.All())
}

func newObservedConformanceServer(
	t *testing.T,
	specField string,
) (*httptest.Server, *observedRequests) {
	t.Helper()
	requests := &observedRequests{}

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requests.Add(r.Method, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/":
				if specField != "" {
					fmt.Fprintf(w, "{\n  %s,\n", specField)
				} else {
					fmt.Fprintln(w, "{")
				}
				fmt.Fprintf(w, `  "registryid": "test",
  "self": "http://%s/",
  "xid": "/",
  "epoch": 1,
  "createdat": "2026-01-01T00:00:00Z",
  "modifiedat": "2026-01-01T00:00:00Z"
}`, r.Host)
			case "/model":
				io.WriteString(w, "{}")
			case "/capabilities":
				io.WriteString(w, `{
  "available": {
    "capabilities": {"mutable": true},
    "entities": {"mutable": true},
    "model": {"mutable": false}
  },
  "compatibilities": {},
  "flags": [],
  "formats": [],
  "ignores": [],
  "pagination": false,
  "shortself": false,
  "specversions": ["1.0-rc4"],
  "versionmodes": []
}`)
			default:
				http.NotFound(w, r)
			}
		}))
	t.Cleanup(server.Close)
	return server, requests
}

func conformanceUnsafeRequestProbe(td *TD) {
	reg := td.GetRegistry()
	res, xErr := reg.HttpDo(false, http.MethodPost, "/mutate", nil)
	td.NoErrorStop(xErr, "POST request MUST be permitted")
	td.HTTPStatusMustEqual(res, http.StatusOK, "POST /mutate")
}

func conformanceNoopProbe(td *TD) {
	td.Pass("No-op probe")
}

func syntheticRequestCase(mode CaseMode) ConformanceCase {
	testFn := TestFn(conformanceUnsafeRequestProbe)
	return ConformanceCase{
		ID:           "core.method-probe",
		FunctionName: testFn.DisplayName(),
		Name:         "Method probe",
		Description:  "Exercise the conformance request guard.",
		ProfileID:    coreConformanceProfileID,
		Mode:         mode,
		Test:         testFn,
		SpecReferences: []SpecReference{
			newSpecReference(coreConformanceProfile, "core/http.md",
				"registry-http-apis", "Registry HTTP APIs"),
		},
	}
}

func newMethodProbeServer(
	t *testing.T,
) (*httptest.Server, *observedRequests) {
	t.Helper()
	requests := &observedRequests{}
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requests.Add(r.Method, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, "{}")
		}))
	t.Cleanup(server.Close)
	return server, requests
}

func newConformanceInvocationTestCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "conform"}
	cmd.Flags().String("server", "", "")
	cmd.Flags().Bool("logs", false, "")
	cmd.Flags().Int("depth", 2, "")
	cmd.Flags().Bool("failfast", false, "")
	cmd.Flags().Bool("nowrap", false, "")
	cmd.Flags().String("run", "", "")
	cmd.Flags().Bool("tdDebug", false, "")
	cmd.Flags().Bool("list-tests", false, "")
	cmd.Flags().StringArray("test", nil, "")
	cmd.Flags().Bool("allow-mutations", false, "")
	return cmd
}
