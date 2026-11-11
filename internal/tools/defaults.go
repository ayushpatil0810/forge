package tools

// DefaultTools returns a registry with standard workspace tools (read-only, mutating, and shell).
func DefaultTools(workspaceRoot string, shells ...ShellConfig) *Registry {
	tracker := NewReadTracker()
	var shell ShellConfig
	if len(shells) > 0 {
		shell = shells[0]
	}
	if shell.Path == "" {
		shell = DetectShell("")
	}
	return NewRegistry(
		ReadFile{Root: workspaceRoot, Tracker: tracker},
		WriteFile{Root: workspaceRoot, Tracker: tracker},
		EditFile{Root: workspaceRoot, Tracker: tracker},
		ListDir{Root: workspaceRoot},
		Glob{Root: workspaceRoot},
		Grep{Root: workspaceRoot},
		Bash{Root: workspaceRoot, Shell: shell},
	)
}

