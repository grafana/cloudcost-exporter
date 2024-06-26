package aks

import (
	"testing"
)

// TODO - mock
// var (
// 	parentCtx  context.Context                    = context.TODO()
// 	testLogger *slog.Logger                       = slog.New(slog.NewTextHandler(os.Stdout, nil))
// 	fakeCreds  *azidentity.DefaultAzureCredential = &azidentity.DefaultAzureCredential{}
// 	testSubId  string                             = "1234-asdf-adsf-adsf"
// )

func Test_New(t *testing.T) {
	// TODO - mock
	t.Skip()

	// for _, tc := range []struct {
	// 	name           string
	// 	subscriptionId string
	// 	expectedError  error
	// }{
	// 	{
	// 		subscriptionId: testSubId,
	// 		name:           "no error",
	// 	},
	// } {
	// 	t.Run(tc.name, func(t *testing.T) {
	// 		c, err := New(parentCtx, &Config{
	// 			Logger:         testLogger,
	// 			SubscriptionId: tc.subscriptionId,
	// 			Credentials:    fakeCreds,
	// 		})
	// 		require.NotNil(t, c)
	// 		if tc.expectedError != nil {
	// 			require.NotNil(t, err)
	// 		}
	// 	})
	// }
}
