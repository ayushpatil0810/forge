package tui

import (
	"os"
	"path/filepath"
	"strings"
)

// DetectGitBranch reads the current git branch name from .git/HEAD.
func DetectGitBranch(root string) string {
	if root == "" {
		root = "."
	}
	headPath := filepath.Join(root, ".git", "HEAD")
	data, err := os.ReadFile(headPath)
	if err != nil {
		// Could be a worktree/submodule with gitdir pointer
		gitFile := filepath.Join(root, ".git")
		gitFileData, err2 := os.ReadFile(gitFile)
		if err2 != nil {
			return ""
		}
		gitStr := strings.TrimSpace(string(gitFileData))
		if strings.HasPrefix(gitStr, "gitdir: ") {
			targetDir := strings.TrimPrefix(gitStr, "gitdir: ")
			if !filepath.IsAbs(targetDir) {
				targetDir = filepath.Join(root, targetDir)
			}
			data, err = os.ReadFile(filepath.Join(targetDir, "HEAD"))
			if err != nil {
				return ""
			}
		} else {
			return ""
		}
	}

	s := strings.TrimSpace(string(data))
	if strings.HasPrefix(s, "ref: refs/heads/") {
		return strings.TrimPrefix(s, "ref: refs/heads/")
	}
	if len(s) >= 7 {
		return s[:7]
	}
	return ""
}
