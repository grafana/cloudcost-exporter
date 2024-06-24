package aks

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	parentCtx  context.Context = context.TODO()
	testLogger *slog.Logger    = slog.New(slog.NewTextHandler(os.Stdout, nil))
)

func Test_New(t *testing.T) {
	for _, tc := range []struct {
		name          string
		expectedError error
	}{
		{
			name: "no error",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(parentCtx, &Config{
				Logger: testLogger,
			})
			require.NotNil(t, c)
		})
	}
}
