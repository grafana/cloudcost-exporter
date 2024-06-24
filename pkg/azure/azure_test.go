package azure

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
			a, err := New(parentCtx, &Config{
				Logger: testLogger,
			})
			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
				return
			}
			require.NoError(t, err)
			require.NotNil(t, a)
		})
	}
}
