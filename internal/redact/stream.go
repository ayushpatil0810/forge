package redact

import (
	"os"
	"strings"
)

// Stream emits complete words and retains the unfinished credential candidates.
type Stream struct {
	buf     strings.Builder
	emit    func(string)
	discard bool
}

const streamHoldMax = 64 * 1024

func NewStream(emit func(string)) *Stream {
	if emit == nil {
		emit = func(string) {}
	}
	return &Stream{emit: emit}
}

func (s *Stream) Write(chunk string) {
	if s == nil || s.discard || chunk == "" {
		return
	}
	s.buf.WriteString(chunk)
	s.drain(false)
}

func (s *Stream) Flush() {
	if s != nil {
		s.drain(true)
	}
}

func (s *Stream) drain(flush bool) {
	data := s.buf.String()
	cut := len(data)
	if !flush {
		cut = commitIndex(data)
	}
	if cut > 0 {
		s.buf.Reset()
		s.buf.WriteString(data[cut:])
		s.emit(Redact(data[:cut]))
	}
	if !flush && s.buf.Len() > streamHoldMax {
		// shortcut: omit the rest of an oversized stream, add structured parsing to recover its safe suffix.
		s.buf.Reset()
		s.discard = true
		s.emit("[REDACTED:oversized]")
	}
}

func commitIndex(data string) int {
	cut := strings.LastIndexAny(data, " \t\r\n") + 1
	for cut > 0 {
		safe := cut
		for offset := 0; offset < safe; {
			begin := strings.Index(data[offset:safe], "-----BEGIN ")
			if begin < 0 {
				break
			}
			begin += offset
			match := rePrivateKey.FindStringSubmatchIndex(data[begin:])
			if match == nil || match[0] != 0 || match[2] < 0 || begin+match[1] > safe {
				safe = begin
				break
			}
			offset = begin + match[1]
		}

		registeredMu.RLock()
		for _, secret := range registeredSecrets {
			safe = min(safe, incompleteSecretStart(data[:cut], secret))
		}
		registeredMu.RUnlock()
		for _, name := range secretEnv {
			if secret := strings.TrimSpace(os.Getenv(name)); len(secret) >= 8 {
				safe = min(safe, incompleteSecretStart(data[:cut], secret))
			}
		}
		if safe == cut {
			break
		}
		cut = safe
	}
	return cut
}

func incompleteSecretStart(prefix, secret string) int {
	for length := min(len(secret)-1, len(prefix)); length > 0; length-- {
		if strings.HasSuffix(prefix, secret[:length]) {
			return len(prefix) - length
		}
	}
	return len(prefix)
}
