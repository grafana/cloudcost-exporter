package client

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// IsRetryableError reports whether err represents a transient gRPC failure that
// is worth retrying. Auth and configuration errors are not retryable.
func IsRetryableError(err error) bool {
	var grpcErr interface {
		GRPCStatus() *status.Status
	}
	if !errors.As(err, &grpcErr) {
		return false
	}
	switch grpcErr.GRPCStatus().Code() {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Internal, codes.ResourceExhausted:
		return true
	default:
		return false
	}
}
