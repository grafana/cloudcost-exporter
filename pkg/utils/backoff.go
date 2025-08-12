package utils

import (
	"time"
)

// Retry runs op up to attempts times, sleeping with exponential backoff between
// retries when op returns a non-nil error and retryIf(err) returns true.
// The delay grows as: initialDelay * 2^attempt, capped at maxDelay.
// If attempts <= 0, it defaults to 1 (no retries).
func Retry(attempts int, initialDelay, maxDelay time.Duration, retryIf func(error) bool, op func() error) error {
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := op(); err != nil {
			lastErr = err
			if !retryIf(err) || attempt == attempts-1 {
				return err
			}
			// exponential backoff with cap
			delay := initialDelay << attempt
			if delay > maxDelay {
				delay = maxDelay
			}
			time.Sleep(delay)
			continue
		}
		return nil
	}
	return lastErr
}
