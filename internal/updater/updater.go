package updater

// DiffResult holds what changed between two versions of a source.
type DiffResult struct {
	NewSymbols     []string
	ChangedSymbols []string
	RemovedSymbols []string
	NewPages       []string
	ChangedPages   []string
	RemovedPages   []string
}
