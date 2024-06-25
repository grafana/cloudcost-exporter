package aks

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/stretchr/testify/require"
)

var (
	parentCtx  context.Context                    = context.TODO()
	testLogger *slog.Logger                       = slog.New(slog.NewTextHandler(os.Stdout, nil))
	fakeCreds  *azidentity.DefaultAzureCredential = &azidentity.DefaultAzureCredential{}
	testSubId  string                             = "1234-asdf-adsf-adsf"
)

func Test_New(t *testing.T) {
	for _, tc := range []struct {
		name           string
		subscriptionId string
		expectedError  error
	}{
		{
			subscriptionId: testSubId,
			name:           "no error",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New(parentCtx, &Config{
				Logger:         testLogger,
				SubscriptionId: tc.subscriptionId,
				Credentials:    fakeCreds,
			})
			require.NotNil(t, c)
			if tc.expectedError != nil {
				require.NotNil(t, err)
			}
		})
	}
}
