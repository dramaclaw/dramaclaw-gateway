package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestGetTaskAdaptorSupportsComfyUIChannelIDs(t *testing.T) {
	require.Equal(t, 63, constant.ChannelTypeComfyUI)
	require.NotNil(t, GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeComfyUI))))
}
