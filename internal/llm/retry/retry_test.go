package retry_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ayushpatil0810/forge/internal/llm/retry"
)

func TestRetrySuccessOnFirstAttempt(t *testing.T) {
	cfg := retry.Config{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	}

	calls := 0
	resp, err := retry.Do(context.Background(), cfg, func(ctx context.Context) (*http.Response, bool, time.Duration, error) {
		calls++
		return &http.Response{StatusCode: 200}, false, 0, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.StatusCode != 200 {
		t.Fatalf("expected status 200 response")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestRetrySuccessAfterTransientErrors(t *testing.T) {
	cfg := retry.Config{
		MaxRetries:     3,
		InitialBackoff: 5 * time.Millisecond,
		MaxBackoff:     20 * time.Millisecond,
	}

	var attempts atomic.Int32
	resp, err := retry.Do(context.Background(), cfg, func(ctx context.Context) (*http.Response, bool, time.Duration, error) {
		att := attempts.Add(1)
		if att < 3 {
			return nil, true, 0, errors.New("temporary 429 error")
		}
		return &http.Response{StatusCode: 200}, false, 0, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 OK after retries")
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestRetryFailsFastOnNonRetryable(t *testing.T) {
	cfg := retry.Config{
		MaxRetries:     3,
		InitialBackoff: 5 * time.Millisecond,
		MaxBackoff:     20 * time.Millisecond,
	}

	calls := 0
	expectedErr := errors.New("401 unauthorized")
	_, err := retry.Do(context.Background(), cfg, func(ctx context.Context) (*http.Response, bool, time.Duration, error) {
		calls++
		return nil, false, 0, expectedErr
	})

	if err != expectedErr {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 call for non-retryable error, got %d", calls)
	}
}

func TestRetryExceedsMaxRetries(t *testing.T) {
	cfg := retry.Config{
		MaxRetries:     2,
		InitialBackoff: 2 * time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}

	calls := 0
	expectedErr := errors.New("500 internal server error")
	_, err := retry.Do(context.Background(), cfg, func(ctx context.Context) (*http.Response, bool, time.Duration, error) {
		calls++
		return nil, true, 0, expectedErr
	})

	if err != expectedErr {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
	// Initial attempt + 2 retries = 3 calls
	if calls != 3 {
		t.Fatalf("expected 3 calls (initial + 2 retries), got %d", calls)
	}
}

func TestRetryCancelsDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := retry.Config{
		MaxRetries:     3,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     2 * time.Second,
	}

	calls := 0
	start := time.Now()
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := retry.Do(ctx, cfg, func(ctx context.Context) (*http.Response, bool, time.Duration, error) {
		calls++
		return nil, true, 0, errors.New("503 unavailable")
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatalf("cancellation during backoff took too long: %v", time.Since(start))
	}
}

func TestIsRetryableStatus(t *testing.T) {
	retryable := []int{429, 529, 500, 502, 503, 504}
	for _, code := range retryable {
		if !retry.IsRetryableStatus(code) {
			t.Errorf("expected code %d to be retryable", code)
		}
	}

	notRetryable := []int{200, 201, 400, 401, 403, 404, 422}
	for _, code := range notRetryable {
		if retry.IsRetryableStatus(code) {
			t.Errorf("expected code %d to NOT be retryable", code)
		}
	}
}

