package client

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsRetryableError(t *testing.T) {
	retryableCodes := []codes.Code{
		codes.Unavailable,
		codes.DeadlineExceeded,
		codes.Internal,
		codes.ResourceExhausted,
	}
	nonRetryableCodes := []codes.Code{
		codes.PermissionDenied,
		codes.Unauthenticated,
		codes.NotFound,
		codes.InvalidArgument,
		codes.AlreadyExists,
		codes.OK,
	}

	for _, code := range retryableCodes {
		t.Run(code.String(), func(t *testing.T) {
			err := status.Errorf(code, "transient error")
			assert.True(t, IsRetryableError(err))
		})
	}

	for _, code := range nonRetryableCodes {
		t.Run(code.String(), func(t *testing.T) {
			err := status.Errorf(code, "non-retryable error")
			assert.False(t, IsRetryableError(err))
		})
	}

	t.Run("non-gRPC error", func(t *testing.T) {
		assert.False(t, IsRetryableError(errors.New("plain error")))
	})

	t.Run("nil error", func(t *testing.T) {
		assert.False(t, IsRetryableError(nil))
	})
}
