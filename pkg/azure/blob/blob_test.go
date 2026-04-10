package blob

import (
	"log/slog"
	"os"
	"strings"
	"testing"

	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func TestCollector_Describe(t *testing.T) {
	c, err := New(&Config{Logger: testLogger})
	require.NoError(t, err)
	ch := make(chan *prometheus.Desc, 4)
	require.NoError(t, c.Describe(ch))
	close(ch)
	var got []string
	for d := range ch {
		got = append(got, d.String())
	}
	require.Len(t, got, 1)
	joined := strings.Join(got, " ")
	prefix := cloudcost_exporter.MetricPrefix + "_azure_blob_"
	assert.Contains(t, joined, prefix+"storage_by_location_usd_per_gibyte_hour")
}

func TestCollector_Register(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	reg := mock_provider.NewMockRegistry(ctrl)
	reg.EXPECT().MustRegister(gomock.Any()).Times(1)

	c, err := New(&Config{Logger: testLogger})
	assert.NoError(t, err)
	assert.NoError(t, c.Register(reg))
}
