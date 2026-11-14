package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	isolatedTempDir string
	isoMu           sync.Mutex
)

// IsolateTestEnvironment isolates HOME, USERPROFILE, APPDATA, LOCALAPPDATA,
// XDG_CONFIG_HOME, XDG_DATA_HOME, and XDG_CACHE_HOME to a temporary directory.
// It returns the temporary directory path and a cleanup function.
func IsolateTestEnvironment() (string, func()) {
	isoMu.Lock()
	defer isoMu.Unlock()

	if inherited := os.Getenv("FORGE_ISOLATED_TEST_DIR"); inherited != "" {
		if fi, err := os.Stat(inherited); err == nil && fi.IsDir() {
			isolatedTempDir = inherited
			return inherited, func() {}
		}
	}

	dir, err := os.MkdirTemp("", "forge-test-home-*")
	if err != nil {
		panic(fmt.Sprintf("failed to create isolated temp home: %v", err))
	}
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realDir
	}
	isolatedTempDir = dir

	origEnv := map[string]string{
		"HOME":                    os.Getenv("HOME"),
		"USERPROFILE":             os.Getenv("USERPROFILE"),
		"APPDATA":                 os.Getenv("APPDATA"),
		"LOCALAPPDATA":            os.Getenv("LOCALAPPDATA"),
		"XDG_CONFIG_HOME":         os.Getenv("XDG_CONFIG_HOME"),
		"XDG_DATA_HOME":           os.Getenv("XDG_DATA_HOME"),
		"XDG_CACHE_HOME":          os.Getenv("XDG_CACHE_HOME"),
		"FORGE_ISOLATED_TEST_DIR": os.Getenv("FORGE_ISOLATED_TEST_DIR"),
	}

	appData := filepath.Join(dir, "AppData", "Roaming")
	localAppData := filepath.Join(dir, "AppData", "Local")
	xdgConfig := filepath.Join(dir, ".config")
	xdgData := filepath.Join(dir, ".local", "share")
	xdgCache := filepath.Join(dir, ".cache")
	macConfig := filepath.Join(dir, "Library", "Application Support")

	_ = os.Setenv("FORGE_ISOLATED_TEST_DIR", dir)
	_ = os.Setenv("HOME", dir)
	_ = os.Setenv("USERPROFILE", dir)
	_ = os.Setenv("APPDATA", appData)
	_ = os.Setenv("LOCALAPPDATA", localAppData)
	_ = os.Setenv("XDG_CONFIG_HOME", xdgConfig)
	_ = os.Setenv("XDG_DATA_HOME", xdgData)
	_ = os.Setenv("XDG_CACHE_HOME", xdgCache)

	_ = os.MkdirAll(appData, 0o700)
	_ = os.MkdirAll(localAppData, 0o700)
	_ = os.MkdirAll(xdgConfig, 0o700)
	_ = os.MkdirAll(xdgData, 0o700)
	_ = os.MkdirAll(xdgCache, 0o700)
	_ = os.MkdirAll(macConfig, 0o700)

	cleanup := func() {
		isoMu.Lock()
		defer isoMu.Unlock()
		for k, v := range origEnv {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
		_ = os.RemoveAll(dir)
	}

	return dir, cleanup
}

// AssertIsolatedHome verifies that os.UserHomeDir() and os.UserConfigDir()
// resolve strictly within the isolated temp directory.
func AssertIsolatedHome(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get os.UserHomeDir(): %v", err)
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("failed to get os.UserConfigDir(): %v", err)
	}

	isoMu.Lock()
	iso := isolatedTempDir
	isoMu.Unlock()

	if iso == "" {
		t.Fatalf("GUARD FAILURE: test environment was not isolated with testutil.IsolateTestEnvironment()")
	}

	normIso := filepath.Clean(iso)
	if realIso, err := filepath.EvalSymlinks(iso); err == nil {
		normIso = realIso
	}
	normHome := filepath.Clean(home)
	if realHome, err := filepath.EvalSymlinks(home); err == nil {
		normHome = realHome
	}
	normCfg := filepath.Clean(cfgDir)
	if realCfg, err := filepath.EvalSymlinks(cfgDir); err == nil {
		normCfg = realCfg
	}

	relHome, errHome := filepath.Rel(normIso, normHome)
	if errHome != nil || strings.HasPrefix(relHome, "..") {
		cleanIso := filepath.Clean(iso)
		cleanHome := filepath.Clean(home)
		relClean, errClean := filepath.Rel(cleanIso, cleanHome)
		if (errClean != nil || strings.HasPrefix(relClean, "..")) && !strings.HasPrefix(cleanHome, cleanIso) {
			t.Fatalf("GUARD FAILURE: os.UserHomeDir() %q resolved outside isolated test directory %q", home, iso)
		}
	}

	relCfg, errCfg := filepath.Rel(normIso, normCfg)
	if errCfg != nil || strings.HasPrefix(relCfg, "..") {
		cleanIso := filepath.Clean(iso)
		cleanCfg := filepath.Clean(cfgDir)
		relClean, errClean := filepath.Rel(cleanIso, cleanCfg)
		if (errClean != nil || strings.HasPrefix(relClean, "..")) && !strings.HasPrefix(cleanCfg, cleanIso) {
			t.Fatalf("GUARD FAILURE: os.UserConfigDir() %q resolved outside isolated test directory %q", cfgDir, iso)
		}
	}
}
