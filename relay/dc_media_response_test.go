package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDCMediaTaskResponseIncludesCanonicalAndLegacyFields(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_public",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Properties: model.Properties{
			OriginModelName: "example-video-model",
		},
		PrivateData: model.TaskPrivateData{ResultURL: "https://example.com/result.mp4"},
	}
	legacy := map[string]any{"status": "succeeded", "task_id": task.TaskID}

	response := buildDCMediaTaskResponse(task, legacy)
	assert.Equal(t, task.TaskID, response["id"])
	assert.Equal(t, task.TaskID, response["task_id"])
	assert.Equal(t, "succeeded", response["status"])
	assert.Equal(t, 100, response["progress"])
	assert.Equal(t, "success", response["code"])
	require.Len(t, response["results"], 1)
	assert.Equal(t, legacy, response["data"])
}

func TestBuildDCMediaTaskResponseUsesStableTerminalStates(t *testing.T) {
	assert.Equal(t, "cancelled", mapTaskStatusToDCMedia(model.TaskStatusCancelled))
	assert.Equal(t, "failed", mapTaskStatusToDCMedia(model.TaskStatusFailure))
	assert.Equal(t, "running", mapTaskStatusToDCMedia(model.TaskStatusInProgress))
	assert.Equal(t, 100, taskProgressPercent("125%"))
}
