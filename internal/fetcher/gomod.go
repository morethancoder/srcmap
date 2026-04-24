package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/morethancoder/srcmap/internal/httpx"
)

// GoModRegistry resolves Go modules to their repository.
type GoModRegistry struct {
	BaseURL string
	Client  *http.Client
}

func (r *GoModRegistry) baseURL() string {
	if r.BaseURL != "" {
		return r.BaseURL
	}
	return "https://proxy.golang.org"
}

func (r *GoModRegistry) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return httpx.Default()
}

// Resolve looks up a Go module and returns its repo URL and latest version.
func (r *GoModRegistry) Resolve(ctx context.Context, name string) (*RegistryResult, error) {
	// Get latest version from Go module proxy
	version, err := r.fetchLatestVersion(ctx, name)
	if err != nil {
		return nil, err
	}

	// Derive repo URL from module path
	repoURL := goModuleToRepoURL(name)

	return &RegistryResult{
		Name:    name,
		RepoURL: repoURL,
		Version: version,
	}, nil
}

func (r *GoModRegistry) fetchLatestVersion(ctx context.Context, module string) (string, error) {
	url := fmt.Sprintf("%s/%s/@latest", r.baseURL(), module)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching Go proxy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Go proxy returned %d for %q", resp.StatusCode, module)
	}

	var info struct {
		Version string `json:"Version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("decoding Go proxy response: %w", err)
	}

	return info.Version, nil
}

// goModuleToRepoURL converts a Go module path to an HTTPS repo URL.
func goModuleToRepoURL(module string) string {
	// Handle well-known hosts
	parts := strings.SplitN(module, "/", 4)
	if len(parts) >= 3 {
		host := parts[0]
		switch host {
		case "github.com", "gitlab.com", "bitbucket.org":
			return fmt.Sprintf("https://%s/%s/%s", host, parts[1], parts[2])
		}
	}
	// Fallback: assume HTTPS
	return "https://" + module
}
