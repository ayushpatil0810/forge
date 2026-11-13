package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ayushpatil0810/forge/internal/audit"
)

func newAuditCmd() *cobra.Command {
	var (
		rootDir   string
		pathFlag  string
		sessionID string
		tailCount int
	)

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Inspect and verify audit logs (tamper-evident against edits, deletions and truncation, not against rewriting both files)",
	}
	cmd.PersistentFlags().StringVar(&rootDir, "root", ".", "workspace root directory")
	cmd.PersistentFlags().StringVar(&pathFlag, "path", "", "override path to audit log file")
	cmd.PersistentFlags().StringVar(&sessionID, "session", "", "filter records by session ID")

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Display audit log entries with redacted secrets and execution summaries",
		RunE: func(c *cobra.Command, _ []string) error {
			targetPath := pathFlag
			if targetPath == "" {
				def, err := defaultAuditPath(rootDir)
				if err != nil {
					return err
				}
				targetPath = def
			}

			records, err := audit.ReadRecords(targetPath, sessionID, tailCount)
			if err != nil {
				return fmt.Errorf("reading audit log: %w", err)
			}

			if len(records) == 0 {
				c.Println("No audit records found.")
				return nil
			}

			c.Printf("Audit Log Records (%s):\n\n", targetPath)
			for _, r := range records {
				c.Printf("[%s] Seq:%d | Session:%s | Event:%s | Tool:%s\n", r.Timestamp, r.Seq, r.SessionID, r.Event, r.Tool)
				if r.ActionSummary != "" {
					c.Printf("  Action:   %s\n", r.ActionSummary)
				}
				if r.Decision != "" {
					c.Printf("  Decision: %s (source: %s)\n", r.Decision, r.DecisionSource)
				}
				if r.ExitCode != nil {
					dur := int64(0)
					if r.DurationMs != nil {
						dur = *r.DurationMs
					}
					c.Printf("  Result:   exit code %d (%dms)\n", *r.ExitCode, dur)
				}
				if r.OutputSHA256 != "" {
					size := 0
					if r.OutputSize != nil {
						size = *r.OutputSize
					}
					c.Printf("  Output:   %d bytes (sha256: %s)\n", size, r.OutputSHA256)
				}
				c.Printf("  Chain:    prev=%s... this=%s...\n\n", r.PrevSHA256[:16], r.RecordSHA256[:16])
			}
			return nil
		},
	}
	showCmd.Flags().IntVar(&tailCount, "tail", 50, "maximum number of recent records to display")

	verifyCmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify the hash chain and that the log was not truncated",
		RunE: func(c *cobra.Command, _ []string) error {
			targetPath := pathFlag
			if targetPath == "" {
				def, err := defaultAuditPath(rootDir)
				if err != nil {
					return err
				}
				targetPath = def
			}

			count, err := audit.Verify(targetPath, sessionID)
			if err != nil {
				if errors.Is(err, audit.ErrTampered) {
					c.PrintErrf("TAMPER DETECTED: %v\n", err)
					os.Exit(1)
				}
				return fmt.Errorf("verifying audit log: %w", err)
			}

			c.Printf("Audit log integrity verified successfully (%d records verified, hash chain intact).\n", count)
			return nil
		},
	}

	cmd.AddCommand(showCmd, verifyCmd)
	return cmd
}

func defaultAuditPath(wsRoot string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("user home directory not found")
	}

	cleanRoot, err := filepath.Abs(wsRoot)
	if err != nil {
		cleanRoot = wsRoot
	}
	hash := sha256.Sum256([]byte(strings.ToLower(cleanRoot)))
	hashHex := hex.EncodeToString(hash[:16])

	return filepath.Join(home, ".forge", "audit", hashHex+".jsonl"), nil
}
