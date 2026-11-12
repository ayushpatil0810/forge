package retry

import (
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultMaxRetries     = 3
	DefaultInitialBackoff = 200 * time.Millisecond
	DefaultMaxBackoff     = 5 * time.Second
)

type OnRetryFunc func(attempt, maxAttempts int, delay time.Duration, err error)

type onRetryContextKey struct{}

// ContextWithOnRetry returns a new context with the given retry callback.
func ContextWithOnRetry(ctx context.Context, fn OnRetryFunc) context.Context {
	return context.WithValue(ctx, onRetryContextKey{}, fn)
}

// OnRetryFromContext retrieves the OnRetryFunc from context if set.
func OnRetryFromContext(ctx context.Context) OnRetryFunc {
	if fn, ok := ctx.Value(onRetryContextKey{}).(OnRetryFunc); ok {
		return fn
	}
	return nil
}

type Config struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	OnRetry        OnRetryFunc
}

func DefaultConfig() Config {
	return Config{
		MaxRetries:     DefaultMaxRetries,
		InitialBackoff: DefaultInitialBackoff,
		MaxBackoff:     DefaultMaxBackoff,
	}
}

// IsRetryableStatus returns true if an HTTP status code represents a transient,
// retryable error per A-8 (429 Rate Limit, 529 Overloaded, and 5xx Server Errors).
// Client errors (400, 401, 403, 404, etc.) return false.
func IsRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code == 529 || (code >= 500 && code <= 599)
}

// ParseRetryAfter parses the Retry-After header into a time.Duration.
// Supports integer seconds. Returns 0 if invalid or unset.
func ParseRetryAfter(val string) time.Duration {
	val = strings.TrimSpace(val)
	if val == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(val); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 0
}

// Operation is a function executed within the retry loop.
// It returns:
// - *http.Response on success
// - retryable bool indicating whether failure can be retried
// - retryAfter time.Duration if the server supplied a Retry-After header
// - error
type Operation func(ctx context.Context) (*http.Response, bool, time.Duration, error)

// Do executes op with exponential backoff, jitter, Retry-After adherence,
// and immediate context cancellation (A-8).
func Do(ctx context.Context, cfg Config, op Operation) (*http.Response, error) {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = DefaultInitialBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = DefaultMaxBackoff
	}

	backoff := cfg.InitialBackoff

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		resp, retryable, retryAfter, err := op(ctx)
		if err == nil {
			return resp, nil
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if !retryable || attempt == cfg.MaxRetries {
			return nil, err
		}

		sleepDuration := retryAfter
		if sleepDuration <= 0 {
			// Calculate exponential backoff with jitter (50% jitter)
			jitter := time.Duration(0)
			if backoff/2 > 0 {
				jitter = time.Duration(rand.Int63n(int64(backoff / 2)))
			}
			sleepDuration = backoff + jitter
			backoff *= 2
			if backoff > cfg.MaxBackoff {
				backoff = cfg.MaxBackoff
			}
		} else if sleepDuration > cfg.MaxBackoff {
			sleepDuration = cfg.MaxBackoff
		}

		onRetry := cfg.OnRetry
		if onRetry == nil {
			onRetry = OnRetryFromContext(ctx)
		}
		if onRetry != nil {
			onRetry(attempt+1, cfg.MaxRetries, sleepDuration, err)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(sleepDuration):
			continue
		}
	}

	return nil, ctx.Err()
}

