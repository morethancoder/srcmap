package updater

// DiffLayer represents changes at one layer of the three-layer fingerprint system.
type DiffLayer struct {
	New     []string
	Changed []string
	Removed []string
}

// IsEmpty returns true if there are no changes in this layer.
func (d *DiffLayer) IsEmpty() bool {
	return len(d.New) == 0 && len(d.Changed) == 0 && len(d.Removed) == 0
}

// TotalChanges returns the total number of changes across all categories.
func (d *DiffLayer) TotalChanges() int {
	return len(d.New) + len(d.Changed) + len(d.Removed)
}

// ComputeDiff computes the difference between stored and current fingerprints.
func ComputeDiff(stored *FingerprintStore, current map[string]string) *DiffLayer {
	newPaths, changed, removed := stored.Compare(current)
	return &DiffLayer{
		New:     newPaths,
		Changed: changed,
		Removed: removed,
	}
}
