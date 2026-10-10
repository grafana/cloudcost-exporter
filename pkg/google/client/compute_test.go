package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

func TestCompute_GetMachineType(t *testing.T) {
	tests := map[string]struct {
		handler http.HandlerFunc
		wantErr error
	}{
		"valid response": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"guestCpus": 2, "memoryMb": 4096}`)) //nolint:errcheck
			},
		},
		"transport error passes through unwrapped": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: nil, // asserted separately below: a raw, non-sentinel error
		},
		"nil body": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`null`)) //nolint:errcheck
			},
			wantErr: ErrNilMachineType,
		},
		"incomplete response, zero GuestCpus": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"guestCpus": 0, "memoryMb": 4096}`)) //nolint:errcheck
			},
			wantErr: ErrIncompleteMachineType,
		},
		"incomplete response, zero MemoryMb": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"guestCpus": 2, "memoryMb": 0}`)) //nolint:errcheck
			},
			wantErr: ErrIncompleteMachineType,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()

			svc, err := compute.NewService(t.Context(), option.WithoutAuthentication(), option.WithEndpoint(server.URL))
			require.NoError(t, err)

			c := newCompute(svc)
			mt, err := c.getMachineType(t.Context(), "project", "zone", "machine-type")

			if name == "transport error passes through unwrapped" {
				require.Error(t, err)
				assert.False(t, errors.Is(err, ErrNilMachineType))
				assert.False(t, errors.Is(err, ErrIncompleteMachineType))
				assert.Nil(t, mt)
				return
			}

			if test.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, test.wantErr))
				assert.Nil(t, mt)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, mt)
			assert.Equal(t, int64(2), mt.GuestCpus)
			assert.Equal(t, int64(4096), mt.MemoryMb)
		})
	}
}
