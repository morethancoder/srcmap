package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// NPMRegistry resolves npm packages to their GitHub repository.
type NPMRegistry struct {
	// BaseURL allows overriding the registry URL for testing.
	BaseURL string
	Client  *http.Client
}

type npmPackageResponse struct {
	Repository struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"repository"`
	DistTags struct {
		Latest string `json:"latest"`
	} `json:"dist-tags"`
}

func (r *NPMRegistry) baseURL() string {
	if r.BaseURL != "" {
		return r.BaseURL
	}
	return "https://registry.npmjs.org"
}

func (r *NPMRegistry) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return http.DefaultClient
}

// Resolve looks up an npm package and returns its repo URL and latest version.
func (r *NPMRegistry) Resolve(ctx context.Context, name string) (*RegistryResult, error) {
	url := fmt.Sprintf("%s/%s", r.baseURL(), name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := r.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching npm registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("npm registry returned %d for %q", resp.StatusCode, name)
	}

	var pkg npmPackageResponse
	if err := json.NewDecoder(resp.Body).Decode(&pkg); err != nil {
		return nil, fmt.Errorf("decoding npm response: %w", err)
	}

	repoURL := normalizeGitURL(pkg.Repository.URL)
	if repoURL == "" {
		return nil, fmt.Errorf("no repository URL found for npm package %q", name)
	}

	return &RegistryResult{
		Name:    name,
		RepoURL: repoURL,
		Version: pkg.DistTags.Latest,
	}, nil
}

// normalizeGitURL converts various git URL formats to HTTPS.
func normalizeGitURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "git+")
	raw = strings.TrimPrefix(raw, "git://")
	raw = strings.TrimSuffix(raw, ".git")

	if strings.HasPrefix(raw, "ssh://") || strings.HasPrefix(raw, "git@") {
		// git@github.com:owner/repo → https://github.com/owner/repo
		raw = strings.TrimPrefix(raw, "ssh://")
		raw = strings.TrimPrefix(raw, "git@")
		raw = strings.Replace(raw, ":", "/", 1)
		raw = "https://" + raw
	}

	if !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "http://") {
		if strings.Contains(raw, "github.com") || strings.Contains(raw, "gitlab.com") {
			raw = "https://" + raw
		}
	}

	return raw
}
