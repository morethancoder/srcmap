package fetcher

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DetectVersion tries to find the installed version of a package from lockfiles
// in the given project directory.
func DetectVersion(projectDir, packageName string) (string, error) {
	detectors := []struct {
		file   string
		detect func(path, pkg string) (string, error)
	}{
		{"package-lock.json", detectFromPackageLock},
		{"pnpm-lock.yaml", detectFromPnpmLock},
		{"yarn.lock", detectFromYarnLock},
		{"poetry.lock", detectFromPoetryLock},
		{"requirements.txt", detectFromRequirementsTxt},
		{"go.sum", detectFromGoSum},
	}

	for _, d := range detectors {
		path := filepath.Join(projectDir, d.file)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		version, err := d.detect(path, packageName)
		if err != nil {
			continue
		}
		if version != "" {
			return version, nil
		}
	}

	return "", fmt.Errorf("version for %q not found in any lockfile", packageName)
}

func detectFromPackageLock(path, pkg string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var lock struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return "", err
	}

	// v3 format: packages["node_modules/pkg"]
	key := "node_modules/" + pkg
	if p, ok := lock.Packages[key]; ok && p.Version != "" {
		return p.Version, nil
	}

	// v1/v2 format: dependencies["pkg"]
	if d, ok := lock.Dependencies[pkg]; ok && d.Version != "" {
		return d.Version, nil
	}

	return "", nil
}

func detectFromPnpmLock(path, pkg string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var lock struct {
		Packages map[string]struct {
			Version string `yaml:"version"`
		} `yaml:"packages"`
	}
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return "", err
	}

	// pnpm uses keys like "/pkg@version" or "pkg@version"
	for key, p := range lock.Packages {
		name := strings.TrimPrefix(key, "/")
		if atIdx := strings.LastIndex(name, "@"); atIdx > 0 {
			if name[:atIdx] == pkg {
				return name[atIdx+1:], nil
			}
		}
		if p.Version != "" && strings.HasPrefix(name, pkg) {
			return p.Version, nil
		}
	}

	return "", nil
}

func detectFromYarnLock(path, pkg string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inPackage := false
	for scanner.Scan() {
		line := scanner.Text()

		// yarn.lock entries look like:
		// "pkg@^version", "pkg@~version":
		//   version "x.y.z"
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			// Header line
			inPackage = strings.Contains(line, pkg+"@")
			continue
		}

		if inPackage {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "version ") {
				version := strings.TrimPrefix(trimmed, "version ")
				version = strings.Trim(version, "\"")
				return version, nil
			}
		}
	}

	return "", scanner.Err()
}

func detectFromPoetryLock(path, pkg string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	// poetry.lock is TOML, but we can do simple line parsing
	lines := strings.Split(string(data), "\n")
	inPackage := false
	nameMatch := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[[package]]" {
			inPackage = true
			nameMatch = false
			continue
		}
		if inPackage {
			if strings.HasPrefix(trimmed, "name = ") {
				name := strings.Trim(strings.TrimPrefix(trimmed, "name = "), "\"")
				nameMatch = strings.EqualFold(name, pkg)
			}
			if nameMatch && strings.HasPrefix(trimmed, "version = ") {
				return strings.Trim(strings.TrimPrefix(trimmed, "version = "), "\""), nil
			}
		}
	}

	return "", nil
}

func detectFromRequirementsTxt(path, pkg string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		// formats: pkg==version, pkg>=version, pkg~=version
		for _, sep := range []string{"==", ">=", "~=", "<=", "!="} {
			if idx := strings.Index(line, sep); idx >= 0 {
				name := strings.TrimSpace(line[:idx])
				if strings.EqualFold(name, pkg) {
					return strings.TrimSpace(line[idx+len(sep):]), nil
				}
			}
		}
	}

	return "", scanner.Err()
}

func detectFromGoSum(path, pkg string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var latestVersion string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// go.sum format: module version hash
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == pkg {
			v := strings.TrimSuffix(parts[1], "/go.mod")
			latestVersion = v
		}
	}

	return latestVersion, scanner.Err()
}
