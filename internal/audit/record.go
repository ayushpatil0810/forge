package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// Record is one audit event. The chain is tamper-evident against edits, deletions
// and truncation, but not against an attacker who can rewrite both the log and its state file.
type Record struct {
	Timestamp      string `json:"timestamp"`
	SessionID      string `json:"session_id"`
	Seq            int    `json:"seq"`
	Event          string `json:"event"` // "tool_requested", "decision", "tool_result"
	Tool           string `json:"tool"`
	ActionSummary  string `json:"action_summary"` // redacted command, or edit summary (path, hashes, line counts); never raw contents or diffs
	Decision       string `json:"decision,omitempty"`
	DecisionSource string `json:"decision_source,omitempty"`
	ExitCode       *int   `json:"exit_code,omitempty"`
	DurationMs     *int64 `json:"duration_ms,omitempty"`
	OutputSize     *int   `json:"output_size,omitempty"`
	OutputSHA256   string `json:"output_sha256,omitempty"`
	PrevSHA256     string `json:"prev_sha256"`
	RecordSHA256   string `json:"record_sha256"`
}

// ComputeRecordHash computes the SHA-256 hash of a record's canonical JSON representation.
func ComputeRecordHash(r Record) (string, error) {
	// Exclude RecordSHA256 from the hash payload
	copyRec := r
	copyRec.RecordSHA256 = ""

	data, err := json.Marshal(copyRec)
	if err != nil {
		return "", err
	}

	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}
