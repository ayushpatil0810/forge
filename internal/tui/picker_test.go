package tui_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/session"
	"github.com/ayushpatil0810/forge/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func sampleSessionsForPicker() []session.SessionSummary {
	refTime := time.Date(2026, 11, 13, 14, 30, 0, 0, time.UTC)
	return []session.SessionSummary{
		{
			ID:           "s_1700000001_abcd",
			Workspace:    "/workspace",
			Provider:     "openai",
			Model:        "gpt-4o",
			Title:        "Fix boundary condition in discount calculation",
			CreatedAt:    refTime.Add(-2 * time.Hour),
			UpdatedAt:    refTime.Add(-10 * time.Minute),
			MessageCount: 8,
			TotalCost:    0.0425,
		},
		{
			ID:           "s_1700000002_efgh",
			Workspace:    "/workspace",
			Provider:     "anthropic",
			Model:        "claude-sonnet-5-5",
			Title:        "Refactor credential management store",
			CreatedAt:    refTime.Add(-24 * time.Hour),
			UpdatedAt:    refTime.Add(-3 * time.Hour),
			MessageCount: 14,
			TotalCost:    0.0890,
		},
		{
			ID:           "s_1700000003_ijkl",
			Workspace:    "/workspace",
			Provider:     "ollama",
			Model:        "llama3.3",
			Title:        "Explore offline model performance",
			CreatedAt:    refTime.Add(-48 * time.Hour),
			UpdatedAt:    refTime.Add(-47 * time.Hour),
			MessageCount: 4,
			TotalCost:    0.0000,
		},
	}
}

func TestSessionPicker_GoldenSnapshots(t *testing.T) {
	sessions := sampleSessionsForPicker()
	picker := tui.NewSessionPicker(sessions)
	styles := tui.NewStyles(tui.DarkTheme())

	widths := []int{60, 80, 120}
	for _, w := range widths {
		rendered := stripANSI(picker.Render(w, styles))
		if len(rendered) == 0 {
			t.Fatalf("empty render output for width %d", w)
		}
		// Save / compare golden snapshot
		goldenFile := filepath.Join("testdata", "golden", "picker", "picker_"+string(rune('0'+w/100))+string(rune('0'+(w%100)/10))+string(rune('0'+w%10))+".golden")
		compareOrUpdateGolden(t, goldenFile, rendered)
	}
}

func TestSessionPicker_Navigation(t *testing.T) {
	sessions := sampleSessionsForPicker()
	picker := tui.NewSessionPicker(sessions)

	// Initial cursor is 0
	if picker.Cursor != 0 {
		t.Fatalf("expected cursor 0, got %d", picker.Cursor)
	}

	// Down arrow moves cursor to 1
	_, done, _ := picker.Update(tea.KeyMsg{Type: tea.KeyDown})
	if done || picker.Cursor != 1 {
		t.Fatalf("expected cursor 1, done=false; got %d, done=%v", picker.Cursor, done)
	}

	// Down arrow moves cursor to 2
	_, done, _ = picker.Update(tea.KeyMsg{Type: tea.KeyDown})
	if done || picker.Cursor != 2 {
		t.Fatalf("expected cursor 2, done=false; got %d, done=%v", picker.Cursor, done)
	}

	// Down arrow at boundary stays at 2
	_, done, _ = picker.Update(tea.KeyMsg{Type: tea.KeyDown})
	if done || picker.Cursor != 2 {
		t.Fatalf("expected cursor 2 at boundary, got %d", picker.Cursor)
	}

	// Up arrow moves back to 1
	_, done, _ = picker.Update(tea.KeyMsg{Type: tea.KeyUp})
	if done || picker.Cursor != 1 {
		t.Fatalf("expected cursor 1, got %d", picker.Cursor)
	}

	// Enter selects session at cursor (sessions[1].ID)
	selectedID, done, _ := picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !done {
		t.Fatal("expected done=true on Enter")
	}
	if selectedID != sessions[1].ID {
		t.Fatalf("expected selected ID %q, got %q", sessions[1].ID, selectedID)
	}

	// Esc aborts with empty ID
	picker2 := tui.NewSessionPicker(sessions)
	selectedID2, done2, _ := picker2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !done2 {
		t.Fatal("expected done=true on Esc")
	}
	if selectedID2 != "" {
		t.Fatalf("expected empty selectedID on Esc, got %q", selectedID2)
	}
}

func TestSessionPicker_Empty(t *testing.T) {
	picker := tui.NewSessionPicker(nil)
	styles := tui.NewStyles(tui.DarkTheme())
	out := picker.Render(80, styles)
	if !strings.Contains(out, "No previous sessions recorded") {
		t.Fatalf("expected empty message, got:\n%s", out)
	}
}
