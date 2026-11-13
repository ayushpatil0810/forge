package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrInvalidPolicy indicates a malformed policy that must terminate execution with code 2.
var ErrInvalidPolicy = errors.New("invalid policy")

type PolicyError struct {
	FilePath string
	Index    int
	Message  string
	Hint     string
}

func (e *PolicyError) Error() string {
	if e.Index >= 0 {
		return fmt.Sprintf("invalid policy file %s (rule #%d): %s; fix hint: %s", e.FilePath, e.Index, e.Message, e.Hint)
	}
	return fmt.Sprintf("invalid policy file %s: %s; fix hint: %s", e.FilePath, e.Message, e.Hint)
}

func (e *PolicyError) Unwrap() error {
	return ErrInvalidPolicy
}

// PolicyFile defines the on-disk JSON structure.
type PolicyFile struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
	Ask   []string `json:"ask,omitempty"`
}

// PolicySet holds loaded user and project rules.
type PolicySet struct {
	UserRules    []Rule   `json:"user_rules"`
	ProjectRules []Rule   `json:"project_rules"`
	Warnings     []string `json:"warnings"`
	UserPath     string   `json:"user_path"`
	ProjectPath  string   `json:"project_path"`
}

// EffectiveRules returns all active rules (user + project).
func (ps *PolicySet) EffectiveRules() []Rule {
	var res []Rule
	res = append(res, ps.UserRules...)
	res = append(res, ps.ProjectRules...)
	return res
}

// Load loads user and project policy files.
// Project policy allow rules are strictly ignored with a warning (untrusted repo defense).
func Load(userPath, projectPath string) (*PolicySet, error) {
	set := &PolicySet{
		UserPath:    userPath,
		ProjectPath: projectPath,
	}

	// 1. Load User Policy (if exists)
	if userPath != "" {
		if _, err := os.Stat(userPath); err == nil {
			userRules, err := parsePolicyFile(userPath, ScopeUser, false)
			if err != nil {
				return nil, err
			}
			set.UserRules = userRules
		}
	}

	// 2. Load Project Policy (if exists)
	if projectPath != "" {
		if _, err := os.Stat(projectPath); err == nil {
			projectRules, warnings, err := parseProjectPolicyFile(projectPath)
			if err != nil {
				return nil, err
			}
			set.ProjectRules = projectRules
			set.Warnings = append(set.Warnings, warnings...)
		}
	}

	return set, nil
}

func parsePolicyFile(filePath string, scope Scope, isProject bool) ([]Rule, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, &PolicyError{
			FilePath: filePath,
			Index:    -1,
			Message:  err.Error(),
			Hint:     "ensure the policy file exists and has read permissions",
		}
	}

	var pf PolicyFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, &PolicyError{
			FilePath: filePath,
			Index:    -1,
			Message:  "JSON parse error: " + err.Error(),
			Hint:     "policy file must be valid JSON: {\"allow\": [...], \"deny\": [...], \"ask\": [...] }",
		}
	}

	var rules []Rule
	idx := 0

	for _, raw := range pf.Allow {
		idx++
		rule, err := ParseRule(ActionAllow, raw, scope, filePath, idx)
		if err != nil {
			return nil, &PolicyError{
				FilePath: filePath,
				Index:    idx,
				Message:  err.Error(),
				Hint:     "check rule format 'tool(pattern)'",
			}
		}
		rules = append(rules, rule)
	}

	for _, raw := range pf.Deny {
		idx++
		rule, err := ParseRule(ActionDeny, raw, scope, filePath, idx)
		if err != nil {
			return nil, &PolicyError{
				FilePath: filePath,
				Index:    idx,
				Message:  err.Error(),
				Hint:     "check rule format 'tool(pattern)'",
			}
		}
		rules = append(rules, rule)
	}

	for _, raw := range pf.Ask {
		idx++
		rule, err := ParseRule(ActionAsk, raw, scope, filePath, idx)
		if err != nil {
			return nil, &PolicyError{
				FilePath: filePath,
				Index:    idx,
				Message:  err.Error(),
				Hint:     "check rule format 'tool(pattern)'",
			}
		}
		rules = append(rules, rule)
	}

	return rules, nil
}

func parseProjectPolicyFile(filePath string) ([]Rule, []string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, &PolicyError{
			FilePath: filePath,
			Index:    -1,
			Message:  err.Error(),
			Hint:     "ensure project policy file has read permissions",
		}
	}

	var pf PolicyFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, nil, &PolicyError{
			FilePath: filePath,
			Index:    -1,
			Message:  "JSON parse error: " + err.Error(),
			Hint:     "project policy must be valid JSON: {\"deny\": [...], \"ask\": [...] }",
		}
	}

	var rules []Rule
	var warnings []string
	idx := 0

	// Project allow rules MUST be ignored with a visible warning!
	for _, raw := range pf.Allow {
		idx++
		// Still validate syntax so syntax errors are caught
		_, err := ParseRule(ActionAllow, raw, ScopeProject, filePath, idx)
		if err != nil {
			return nil, nil, &PolicyError{
				FilePath: filePath,
				Index:    idx,
				Message:  err.Error(),
				Hint:     "check rule format 'tool(pattern)'",
			}
		}
		warn := fmt.Sprintf("warning: project policy allow rule %q ignored; allow rules must be configured in user policy", raw)
		warnings = append(warnings, warn)
	}

	for _, raw := range pf.Deny {
		idx++
		rule, err := ParseRule(ActionDeny, raw, ScopeProject, filePath, idx)
		if err != nil {
			return nil, nil, &PolicyError{
				FilePath: filePath,
				Index:    idx,
				Message:  err.Error(),
				Hint:     "check rule format 'tool(pattern)'",
			}
		}
		rules = append(rules, rule)
	}

	for _, raw := range pf.Ask {
		idx++
		rule, err := ParseRule(ActionAsk, raw, ScopeProject, filePath, idx)
		if err != nil {
			return nil, nil, &PolicyError{
				FilePath: filePath,
				Index:    idx,
				Message:  err.Error(),
				Hint:     "check rule format 'tool(pattern)'",
			}
		}
		rules = append(rules, rule)
	}

	return rules, warnings, nil
}

// AddRule adds a rule to user or project policy and persists it to disk.
func (ps *PolicySet) AddRule(scope Scope, action Action, ruleStr string) error {
	if scope == ScopeProject && action == ActionAllow {
		return errors.New("cannot add allow rules to project policy; project policies may only specify 'deny' or 'ask' rules")
	}

	targetFile := ps.UserPath
	if scope == ScopeProject {
		targetFile = ps.ProjectPath
	}
	if targetFile == "" {
		return errors.New("no policy file path configured for scope: " + string(scope))
	}

	// Validate rule
	rule, err := ParseRule(action, ruleStr, scope, targetFile, 1)
	if err != nil {
		return err
	}

	// Load existing file or init empty
	var pf PolicyFile
	if data, err := os.ReadFile(targetFile); err == nil {
		_ = json.Unmarshal(data, &pf)
	}

	// Append rule if not already present
	switch action {
	case ActionAllow:
		for _, existing := range pf.Allow {
			if existing == rule.Raw {
				return nil
			}
		}
		pf.Allow = append(pf.Allow, rule.Raw)
		ps.UserRules = append(ps.UserRules, rule)
	case ActionDeny:
		for _, existing := range pf.Deny {
			if existing == rule.Raw {
				return nil
			}
		}
		pf.Deny = append(pf.Deny, rule.Raw)
		if scope == ScopeUser {
			ps.UserRules = append(ps.UserRules, rule)
		} else {
			ps.ProjectRules = append(ps.ProjectRules, rule)
		}
	case ActionAsk:
		for _, existing := range pf.Ask {
			if existing == rule.Raw {
				return nil
			}
		}
		pf.Ask = append(pf.Ask, rule.Raw)
		if scope == ScopeUser {
			ps.UserRules = append(ps.UserRules, rule)
		} else {
			ps.ProjectRules = append(ps.ProjectRules, rule)
		}
	}

	return savePolicyFile(targetFile, pf)
}

// RemoveRule removes a rule by raw string or pattern.
func (ps *PolicySet) RemoveRule(scope Scope, ruleStr string) error {
	targetFile := ps.UserPath
	if scope == ScopeProject {
		targetFile = ps.ProjectPath
	}
	if targetFile == "" {
		return errors.New("no policy file path configured for scope: " + string(scope))
	}

	data, err := os.ReadFile(targetFile)
	if err != nil {
		return err
	}

	var pf PolicyFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return err
	}

	clean := func(list []string) []string {
		var res []string
		for _, item := range list {
			if strings.TrimSpace(item) != strings.TrimSpace(ruleStr) {
				res = append(res, item)
			}
		}
		return res
	}

	pf.Allow = clean(pf.Allow)
	pf.Deny = clean(pf.Deny)
	pf.Ask = clean(pf.Ask)

	if err := savePolicyFile(targetFile, pf); err != nil {
		return err
	}

	// Reload in-memory rules
	reloaded, err := Load(ps.UserPath, ps.ProjectPath)
	if err == nil {
		ps.UserRules = reloaded.UserRules
		ps.ProjectRules = reloaded.ProjectRules
		ps.Warnings = reloaded.Warnings
	}
	return nil
}

func savePolicyFile(filePath string, pf PolicyFile) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, append(data, '\n'), 0600)
}
