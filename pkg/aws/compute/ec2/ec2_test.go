package ec2

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

var testLogger *slog.Logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func TestNewEC2Collector(t *testing.T) {
	t.Run("Instance is created", func(t *testing.T) {
		ec2 := New(nil, &Config{
			Logger: testLogger,
		}, nil, nil, nil)
		assert.NotNil(t, ec2)
		assert.Equal(t, subsystem, ec2.Name())
	})
}

func TestCollector_CollectMetrics(t *testing.T) {
	t.Run("Returns 0", func(t *testing.T) {
		ec2 := New(nil, &Config{
			Logger: testLogger,
		}, nil, nil, nil)
		result := ec2.CollectMetrics(nil)
		assert.Equal(t, 0.0, result)
	})
}

func TestCollector_Describe(t *testing.T) {
	t.Run("Returns nil", func(t *testing.T) {
		ec2 := New(nil, &Config{
			Logger: testLogger,
		}, nil, nil, nil)
		result := ec2.Describe(nil)
		assert.Nil(t, result)
	})
}

func TestCollector_Collect(t *testing.T) {
	t.Run("Returns nil", func(t *testing.T) {
		ec2 := New(nil, &Config{
			Logger: testLogger,
		}, nil, nil, nil)
		result := ec2.Collect(nil)
		assert.Nil(t, result)
	})
}

func TestCollector_Register(t *testing.T) {
	t.Run("Runs register", func(t *testing.T) {
		ec2 := New(nil, &Config{
			Logger: testLogger,
		}, nil, nil, nil)
		err := ec2.Register(nil)
		assert.Nil(t, err)
	})
}
