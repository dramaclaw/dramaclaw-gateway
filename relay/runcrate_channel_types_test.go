package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuncrateAppearsInChannelTypesAPI(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeRuncrate)
	require.True(t, ok)
	assert.Equal(t, constant.APITypeRuncrate, apiType)

	var found *ChannelTypeMetadata
	for _, item := range GetRegisteredChannelTypes() {
		if item.Type == constant.ChannelTypeRuncrate {
			item := item
			found = &item
		}
	}
	require.NotNil(t, found, "Runcrate must be discoverable in the channel types API")
	assert.Equal(t, "runcrate", found.Provider)
	assert.Equal(t, "Runcrate", found.Name)
	assert.Equal(t, "https://api.runcrate.ai", found.DefaultBaseURL)
	assert.Equal(t, []string{"text"}, found.Capabilities)
	assert.False(t, found.RequiresBaseURL)
	assert.True(t, found.SupportsBaseURLOverride)
}
