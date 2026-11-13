package redact

import "strings"

// Stream redacts tool output as it arrives. Complete lines are emitted after
// redaction. A trailing window is held back so a secret split across chunks
// is not emitted until it is complete, and Flush emits whatever remains.
type Stream struct {
	buf  strings.Builder
	emit func(string)
}

const (
	streamHoldMax  = 64 * 1024
	streamLookback = 4096
)

// NewStream returns a streamer that calls emit with redacted chunks.
func NewStream(emit func(string)) *Stream {
	if emit == nil {
		emit = func(string) {}
	}
	return &Stream{emit: emit}
}

// Write appends a chunk and emits any prefix that can be redacted safely.
func (s *Stream) Write(chunk string) {
	if s == nil || chunk == "" {
		return
	}
	s.buf.WriteString(chunk)
	s.drain(false)
}

// Flush redacts and emits the buffered remainder.
func (s *Stream) Flush() {
	if s == nil {
		return
	}
	s.drain(true)
}

func (s *Stream) drain(flush bool) {
	data := s.buf.String()
	if data == "" {
		return
	}
	cut := len(data)
	if !flush {
		cut = commitIndex(data)
		if cut <= 0 {
			return
		}
	}
	chunk := data[:cut]
	rest := data[cut:]
	s.buf.Reset()
	s.buf.WriteString(rest)
	if out := Redact(chunk); out != "" {
		s.emit(out)
	}
}

// commitIndex returns how much of data can be redacted and emitted.
// Zero means hold the whole buffer.
func commitIndex(data string) int {
	lastNL := strings.LastIndex(data, "\n")
	if lastNL < 0 {
		if len(data) <= streamHoldMax {
			return 0
		}
		return retreat(data, len(data)-streamLookback)
	}
	cut := lastNL + 1
	if begin := strings.LastIndex(data[:cut], "-----BEGIN "); begin >= 0 {
		end := strings.LastIndex(data[:cut], "-----END ")
		if end < begin {
			if len(data) <= streamHoldMax {
				return 0
			}
			cut = begin
		}
	}
	return retreat(data, cut)
}

// retreat pulls the cut back to the start of a secret that may still be
// incomplete, so Redact sees the whole value.
func retreat(data string, cut int) int {
	if cut <= 0 {
		return 0
	}
	if cut > len(data) {
		cut = len(data)
	}
	start := cut - streamLookback
	if start < 0 {
		start = 0
	}
	best := cut
	markers := []string{
		"sk-", "AKIA", "ASIA", "ghp_", "gho_", "ghu_", "ghs_", "ghr_",
		"github_pat_", "AIza", "-----BEGIN ",
	}
	region := data[start:cut]
	for _, m := range markers {
		if i := strings.LastIndex(region, m); i >= 0 {
			abs := start + i
			if abs < best && !strings.Contains(data[abs:cut], "\n") {
				best = abs
			}
		}
	}
	registeredMu.RLock()
	defer registeredMu.RUnlock()
	for _, sec := range registeredSecrets {
		if len(sec) < 6 {
			continue
		}
		max := len(sec) - 1
		if max > cut {
			max = cut
		}
		low := max - streamLookback
		if low < 6 {
			low = 6
		}
		for n := max; n >= low; n-- {
			if n > cut {
				continue
			}
			if strings.HasSuffix(data[:cut], sec[:n]) {
				abs := cut - n
				if abs < best {
					best = abs
				}
				break
			}
		}
	}
	if best < cut-streamLookback {
		best = cut - streamLookback
	}
	if best < 0 {
		return 0
	}
	return best
}
