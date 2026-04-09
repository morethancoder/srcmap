package updater

import (
	"fmt"
	"strings"
)

// FormatChangelogEntry formats a DiffResult into a human-readable changelog entry.
func FormatChangelogEntry(diff *DiffResult) string {
	var parts []string

	if len(diff.NewSymbols) > 0 {
		parts = append(parts, fmt.Sprintf("Added %d new symbols: %s", len(diff.NewSymbols), strings.Join(diff.NewSymbols, ", ")))
	}
	if len(diff.ChangedSymbols) > 0 {
		parts = append(parts, fmt.Sprintf("Updated %d symbols: %s", len(diff.ChangedSymbols), strings.Join(diff.ChangedSymbols, ", ")))
	}
	if len(diff.RemovedSymbols) > 0 {
		parts = append(parts, fmt.Sprintf("Removed %d symbols: %s", len(diff.RemovedSymbols), strings.Join(diff.RemovedSymbols, ", ")))
	}
	if len(diff.NewPages) > 0 {
		parts = append(parts, fmt.Sprintf("Added %d new doc pages", len(diff.NewPages)))
	}
	if len(diff.ChangedPages) > 0 {
		parts = append(parts, fmt.Sprintf("Updated %d doc pages", len(diff.ChangedPages)))
	}
	if len(diff.RemovedPages) > 0 {
		parts = append(parts, fmt.Sprintf("Removed %d doc pages", len(diff.RemovedPages)))
	}

	if len(parts) == 0 {
		return "No changes detected."
	}

	return strings.Join(parts, "\n")
}

// FormatDiffLayerEntry formats a DiffLayer into a changelog entry.
func FormatDiffLayerEntry(layer *DiffLayer, label string) string {
	var parts []string

	if len(layer.New) > 0 {
		parts = append(parts, fmt.Sprintf("Added %d new %s", len(layer.New), label))
	}
	if len(layer.Changed) > 0 {
		parts = append(parts, fmt.Sprintf("Updated %d %s", len(layer.Changed), label))
	}
	if len(layer.Removed) > 0 {
		parts = append(parts, fmt.Sprintf("Removed %d %s", len(layer.Removed), label))
	}

	return strings.Join(parts, "\n")
}
