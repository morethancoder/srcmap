package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// PyPIRegistry resolves PyPI packages to their repository.
type PyPIRegistry struct {
	BaseURL string
	Client  *http.Client
}

type pypiPackageResponse struct {
	Info struct {
		Version    string            `json:"version"`
		ProjectURL string            `json:"project_url"`
		ProjectURLs map[string]string `json:"project_urls"`
		HomePage   string            `json:"home_page"`
	} `json:"info"`
}

func (r *PyPIRegistry) baseURL() string {
	if r.BaseURL != "" {
		return r.BaseURL
	}
	return "https://pypi.org"
}

func (r *PyPIRegistry) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return http.DefaultClient
}

// Resolve looks up a PyPI package and returns its repo URL and version.
func (r *PyPIRegistry) Resolve(ctx context.Context, name string) (*RegistryResult, error) {
	url := fmt.Sprintf("%s/pypi/%s/json", r.baseURL(), name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching PyPI: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PyPI returned %d for %q", resp.StatusCode, name)
	}

	var pkg pypiPackageResponse
	if err := json.NewDecoder(resp.Body).Decode(&pkg); err != nil {
		return nil, fmt.Errorf("decoding PyPI response: %w", err)
	}

	repoURL := findPyPIRepoURL(pkg)
	if repoURL == "" {
		return nil, fmt.Errorf("no repository URL found for PyPI package %q", name)
	}

	return &RegistryResult{
		Name:    name,
		RepoURL: normalizeGitURL(repoURL),
		Version: pkg.Info.Version,
	}, nil
}

func findPyPIRepoURL(pkg pypiPackageResponse) string {
	// Check project_urls for common repo keys
	for _, key := range []string{"Source", "Source Code", "Repository", "GitHub", "Code", "Homepage"} {
		if url, ok := pkg.Info.ProjectURLs[key]; ok && url != "" {
			return url
		}
	}
	// Fall back to home_page
	return pkg.Info.HomePage
}
