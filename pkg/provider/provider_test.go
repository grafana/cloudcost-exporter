package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCanonicalServiceName(t *testing.T) {
	services := []ServiceInfo{
		{Name: "EVENTHUBS", Aliases: []string{"EVENTHUB"}},
		{Name: "BLOB"},
	}
	for in, want := range map[string]string{
		"EVENTHUBS":  "EVENTHUBS",
		"eventhub":   "EVENTHUBS",
		" EventHub ": "EVENTHUBS",
		"blob":       "BLOB",
		"nope":       "NOPE",
		"":           "",
	} {
		assert.Equal(t, want, CanonicalServiceName(services, in), in)
	}
}
