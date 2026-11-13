package tools

// DefaultTools returns a registry with standard workspace tools (read-only, mutating, and shell).
func DefaultTools(workspaceRoot string, shells ...ShellConfig) *Registry {
	return defaultTools(workspaceRoot, nil, shells...)
}

// DefaultToolsWithEnvAllow is DefaultTools plus the user-config bash.env_allow list.
func DefaultToolsWithEnvAllow(workspaceRoot string, envAllow []string, shells ...ShellConfig) *Registry {
	return defaultTools(workspaceRoot, envAllow, shells...)
}

func defaultTools(workspaceRoot string, envAllow []string, shells ...ShellConfig) *Registry {
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
		Bash{Root: workspaceRoot, Shell: shell, EnvAllow: append([]string(nil), envAllow...)},
	)
}

// DemoTools returns a tool registry tailored for the deterministic demo scenario.
// read_file and edit_file remain real; bash uses DemoBash to run on any host without a Go compiler.
func DemoTools(workspaceRoot string, speed float64) *Registry {
	tracker := NewReadTracker()
	return NewRegistry(
		ReadFile{Root: workspaceRoot, Tracker: tracker},
		WriteFile{Root: workspaceRoot, Tracker: tracker},
		EditFile{Root: workspaceRoot, Tracker: tracker},
		ListDir{Root: workspaceRoot},
		Glob{Root: workspaceRoot},
		Grep{Root: workspaceRoot},
		NewDemoBash(workspaceRoot, speed),
	)
}
