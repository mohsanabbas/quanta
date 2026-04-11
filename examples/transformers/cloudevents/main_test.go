package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	pb "quanta/api/proto/v1"

	cloudevents "github.com/cloudevents/sdk-go/v2/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransformProducesCloudEvent(t *testing.T) {
	raw := `{
		"properties": {
			"request_id": "req-000001-abc123",
			"provider": "anthropic",
			"model": "claude-opus-4",
			"status": "success",
			"input_tokens": 1500,
			"output_tokens": 800,
			"latency_ms": 2300,
			"temperature": 0.7,
			"max_tokens": 4096,
			"stream": true,
			"finish_reason": "stop",
			"origin": "api"
		},
		"context": {
			"event_contract_id": "evt-000001-def456",
			"event": "chat_completion",
			"app_name": "quanta-ai-gateway",
			"app_version": "2.4.0",
			"created_at": "2026-04-03T10:30:00Z",
			"user_id": "usr-00042",
			"org_id": "org-acme",
			"environment": "production"
		}
	}`

	srv := &transformerServer{}
	resp, err := srv.Transform(context.Background(), &pb.TransformRequest{
		Payload: []byte(raw),
	})
	require.NoError(t, err)
	require.Equal(t, pb.Status_OK, resp.GetStatus())
	require.Len(t, resp.GetEvents(), 1)

	ev := resp.GetEvents()[0]
	md := ev.GetMetadata()

	// Verify it's a valid CloudEvent via SDK unmarshaling.
	var ce cloudevents.Event
	require.NoError(t, json.Unmarshal(ev.GetValue(), &ce))

	assert.Equal(t, "1.0", ce.SpecVersion())
	assert.Equal(t, "ai.quanta.chat_completion", ce.Type())
	assert.Equal(t, "/quanta/ai/anthropic/quanta-ai-gateway", ce.Source())
	assert.Equal(t, "claude-opus-4", ce.Subject())
	assert.Equal(t, "evt-000001-def456", ce.ID())
	assert.Equal(t, "2026-04-03T10:30:00Z", ce.Time().Format("2006-01-02T15:04:05Z"))
	assert.Equal(t, "application/json", ce.DataContentType())

	// Extension attributes.
	assert.Equal(t, "anthropic", ce.Extensions()["aiprovider"])
	assert.Equal(t, "production", ce.Extensions()["environment"])
	assert.Equal(t, "success", ce.Extensions()["statusclass"])

	// Data payload.
	var data eventData
	require.NoError(t, json.Unmarshal(ce.Data(), &data))
	assert.Equal(t, 2300, data.TotalTokens)
	assert.Equal(t, "anthropic", data.Provider)
	assert.Equal(t, "stop", data.FinishReason)
	assert.Equal(t, "production", data.Environment)
	assert.Equal(t, "usr-00042", data.UserID)

	// Headers — CloudEvents Kafka protocol binding.
	assert.Equal(t, "ai.quanta.chat_completion", md.Headers["ce-type"])
	assert.Equal(t, "anthropic", md.Headers["ce-aiprovider"])
	assert.Equal(t, "success", md.Headers["ce-statusclass"])

	// Sink key.
	assert.Equal(t, "evt-000001-def456", md.Attributes["sink.key"])
}

func TestTransformDLQOnInvalidJSON(t *testing.T) {
	srv := &transformerServer{}
	resp, err := srv.Transform(context.Background(), &pb.TransformRequest{
		Payload: []byte(`{not valid json`),
	})
	require.NoError(t, err)
	require.Equal(t, pb.Status_OK, resp.GetStatus())
	require.Len(t, resp.GetErrorEvents(), 1)
	require.Empty(t, resp.GetEvents())

	ev := resp.GetErrorEvents()[0]
	md := ev.GetMetadata()

	assert.Equal(t, "unmarshal_error", md.Headers["dlq-error-class"])
	assert.Equal(t, _pluginName, md.Headers["dlq-transformer"])

	// The DLQ payload embeds the raw invalid JSON inside raw_payload,
	// so we parse it as a map to inspect the envelope fields.
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(ev.GetValue(), &envelope))
	assert.Equal(t, "unmarshal_error", envelope["error_class"])
	assert.Equal(t, _pluginName, envelope["transformer"])
}

func TestTransformDLQOnMissingProvider(t *testing.T) {
	raw := `{
		"properties": {"request_id": "req-1", "model": "gpt-4o", "status": "success"},
		"context": {"event_contract_id": "evt-1", "event": "", "app_name": "test"}
	}`

	srv := &transformerServer{}
	resp, err := srv.Transform(context.Background(), &pb.TransformRequest{
		Payload: []byte(raw),
	})
	require.NoError(t, err)
	require.Len(t, resp.GetErrorEvents(), 1)
	require.Empty(t, resp.GetEvents())

	md := resp.GetErrorEvents()[0].GetMetadata()
	assert.Equal(t, "validation_error", md.Headers["dlq-error-class"])
}

func TestTransformDLQOnUnhealthyStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{name: "error status", status: "error"},
		{name: "timeout status", status: "timeout"},
		{name: "rate_limited status", status: "rate_limited"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{
				"properties": {
					"request_id": "req-1", "provider": "openai", "model": "gpt-4o",
					"status": %q, "input_tokens": 100, "output_tokens": 50,
					"latency_ms": 500, "temperature": 0.5, "max_tokens": 1024,
					"stream": false, "finish_reason": "error", "origin": "api"
				},
				"context": {
					"event_contract_id": "evt-1", "event": "chat_completion",
					"app_name": "test", "app_version": "1.0.0",
					"created_at": "2026-04-03T10:00:00Z",
					"user_id": "usr-1", "org_id": "org-1", "environment": "production"
				}
			}`, tt.status)

			srv := &transformerServer{}
			resp, err := srv.Transform(context.Background(), &pb.TransformRequest{
				Payload: []byte(raw),
			})
			require.NoError(t, err)
			require.Len(t, resp.GetErrorEvents(), 1)
			require.Empty(t, resp.GetEvents())

			md := resp.GetErrorEvents()[0].GetMetadata()
			assert.Equal(t, "status_rejected", md.Headers["dlq-error-class"])
		})
	}
}

func TestTransformPassesHealthyStatuses(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{name: "success", status: "success"},
		{name: "content_filtered", status: "content_filtered"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{
				"properties": {
					"request_id": "req-1", "provider": "openai", "model": "gpt-4o",
					"status": %q, "input_tokens": 100, "output_tokens": 50,
					"latency_ms": 500, "temperature": 0.5, "max_tokens": 1024,
					"stream": false, "finish_reason": "stop", "origin": "api"
				},
				"context": {
					"event_contract_id": "evt-1", "event": "chat_completion",
					"app_name": "test", "app_version": "1.0.0",
					"created_at": "2026-04-03T10:00:00Z",
					"user_id": "usr-1", "org_id": "org-1", "environment": "production"
				}
			}`, tt.status)

			srv := &transformerServer{}
			resp, err := srv.Transform(context.Background(), &pb.TransformRequest{
				Payload: []byte(raw),
			})
			require.NoError(t, err)
			require.Len(t, resp.GetEvents(), 1)
			require.Empty(t, resp.GetErrorEvents())

			md := resp.GetEvents()[0].GetMetadata()
			assert.NotContains(t, md.Headers, "__topic")
		})
	}
}
