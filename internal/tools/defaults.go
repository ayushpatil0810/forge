package tools

// DefaultTools returns a registry with standard read-only workspace tools.
func DefaultTools(workspaceRoot string) *Registry {
	return NewRegistry(
		ReadFile{Root: workspaceRoot},
		ListDir{Root: workspaceRoot},
		Glob{Root: workspaceRoot},
		Grep{Root: workspaceRoot},
	)
}
