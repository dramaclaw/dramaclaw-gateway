package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
)

// Runcrate returns a final usage chunk when stream_options.include_usage is set.
func TestRuncrateSupportsStreamOptions(t *testing.T) {
	assert.True(t, streamSupportedChannels[constant.ChannelTypeRuncrate])
}
