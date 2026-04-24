package fetcher_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morethancoder/srcmap/internal/fetcher"
)

func TestNPMRegistryLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zod" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"repository": map[string]string{
				"type": "git",
				"url":  "git+https://github.com/colinhacks/zod.git",
			},
			"dist-tags": map[string]string{
				"latest": "3.22.4",
			},
		})
	}))
	defer srv.Close()

	reg := &fetcher.NPMRegistry{BaseURL: srv.URL}
	result, err := reg.Resolve(context.Background(), "zod")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if result.RepoURL != "https://github.com/colinhacks/zod" {
		t.Errorf("repo URL: got %q, want %q", result.RepoURL, "https://github.com/colinhacks/zod")
	}
	if result.Version != "3.22.4" {
		t.Errorf("version: got %q, want %q", result.Version, "3.22.4")
	}
}

// TestNPMRegistryScopedPackage verifies that scoped npm packages
// (@scope/pkg) hit the registry with the URL-encoded form required by
// npm's API, and that the resolved metadata is parsed correctly.
func TestNPMRegistryScopedPackage(t *testing.T) {
	var gotEscapedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEscapedPath = r.URL.EscapedPath()
		// r.URL.Path is the decoded form — match on that for correctness.
		if r.URL.Path != "/@tma.js/sdk" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"repository": map[string]string{
				"type": "git",
				"url":  "git+https://github.com/Telegram-Mini-Apps/telegram-apps.git",
			},
			"dist-tags": map[string]string{"latest": "2.0.0"},
		})
	}))
	defer srv.Close()

	reg := &fetcher.NPMRegistry{BaseURL: srv.URL}
	result, err := reg.Resolve(context.Background(), "@tma.js/sdk")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// The slash inside a scoped name must be URL-encoded per npm's registry
	// docs; %2F (case-insensitive) is the canonical form.
	lower := strings.ToLower(gotEscapedPath)
	if !strings.Contains(lower, "%2f") {
		t.Errorf("expected URL-encoded slash in path, got %q", gotEscapedPath)
	}
	if result.Version != "2.0.0" {
		t.Errorf("version: got %q, want %q", result.Version, "2.0.0")
	}
}

func TestPyPILookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pypi/requests/json" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"info": map[string]interface{}{
				"version":   "2.31.0",
				"home_page": "https://github.com/psf/requests",
				"project_urls": map[string]string{
					"Source": "https://github.com/psf/requests",
				},
			},
		})
	}))
	defer srv.Close()

	reg := &fetcher.PyPIRegistry{BaseURL: srv.URL}
	result, err := reg.Resolve(context.Background(), "requests")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if result.RepoURL != "https://github.com/psf/requests" {
		t.Errorf("repo URL: got %q, want %q", result.RepoURL, "https://github.com/psf/requests")
	}
	if result.Version != "2.31.0" {
		t.Errorf("version: got %q, want %q", result.Version, "2.31.0")
	}
}

func TestGoModLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"Version": "v1.10.0",
		})
	}))
	defer srv.Close()

	reg := &fetcher.GoModRegistry{BaseURL: srv.URL}
	result, err := reg.Resolve(context.Background(), "github.com/spf13/cobra")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if result.RepoURL != "https://github.com/spf13/cobra" {
		t.Errorf("repo URL: got %q, want %q", result.RepoURL, "https://github.com/spf13/cobra")
	}
	if result.Version != "v1.10.0" {
		t.Errorf("version: got %q, want %q", result.Version, "v1.10.0")
	}
}

func TestLockfileVersionDetection(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")

	tests := []struct {
		name string
		pkg  string
		want string
	}{
		{"package-lock zod", "zod", "3.22.4"},
		{"package-lock typescript", "typescript", "5.3.2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := fetcher.DetectVersion(testdataDir, tt.pkg)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if version != tt.want {
				t.Errorf("got %q, want %q", version, tt.want)
			}
		})
	}
}

func TestYarnLockVersionDetection(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")

	// yarn.lock should be detected as fallback (package-lock.json is checked first)
	version, err := fetcher.DetectVersion(testdataDir, "zod")
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if version != "3.22.4" {
		t.Errorf("got %q, want %q", version, "3.22.4")
	}
}

func TestRequirementsTxtVersionDetection(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")

	version, err := fetcher.DetectVersion(testdataDir, "requests")
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if version != "2.31.0" {
		t.Errorf("got %q, want %q", version, "2.31.0")
	}
}

func TestParsePackageName(t *testing.T) {
	tests := []struct {
		input string
		want  fetcher.PackageType
	}{
		{"zod", fetcher.PackageNPM},
		{"pypi:requests", fetcher.PackagePyPI},
		{"npm:zod", fetcher.PackageNPM},
		{"github.com/spf13/cobra", fetcher.PackageGoMod},
		{"owner/repo", fetcher.PackageGitHub},
		// Scoped npm packages: must route to npm even when the scope contains
		// a "." (regression: used to be misrouted to Go modules / GitHub).
		{"@tma.js/sdk", fetcher.PackageNPM},
		{"@scope/pkg", fetcher.PackageNPM},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			req := fetcher.ParsePackageName(tt.input, false)
			if req.Type != tt.want {
				t.Errorf("got %q, want %q", req.Type, tt.want)
			}
		})
	}
}

func TestParallelFetch(t *testing.T) {
	// Mock registry that returns synthetic results
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"repository": map[string]string{
				"type": "git",
				"url":  "https://github.com/test/test.git",
			},
			"dist-tags": map[string]string{
				"latest": "1.0.0",
			},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	orch := fetcher.NewOrchestrator(dir, filepath.Join(dir, "global"))
	orch.Registries[fetcher.PackageNPM] = &fetcher.NPMRegistry{BaseURL: srv.URL}

	// Use a mock git fetcher that just creates the directory
	orch.GitFetcher = &mockGitFetcher{}

	requests := []fetcher.FetchRequest{
		{Name: "pkg-a", Type: fetcher.PackageNPM},
		{Name: "pkg-b", Type: fetcher.PackageNPM},
		{Name: "pkg-c", Type: fetcher.PackageNPM},
	}

	results := orch.FetchAll(context.Background(), requests)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for i, r := range results {
		if r.Err != nil {
			t.Errorf("result %d: unexpected error: %v", i, r.Err)
		}
	}
}

type mockGitFetcher struct{}

func (f *mockGitFetcher) Fetch(ctx context.Context, repoURL, version, destPath string) error {
	return nil // just pretend we cloned
}
