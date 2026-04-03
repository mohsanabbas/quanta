package main

import (
	"context"
	"encoding/json"
	"testing"

	pb "quanta/api/proto/v1"
)

func TestTransformerProducesNormalizedEvent(t *testing.T) {
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
            "created_at": "2026-04-03T12:00:00Z",
            "user_id": "usr-042000",
            "org_id": "org-acme",
            "environment": "production"
        }
    }`

	srv := &transformerServer{}
	resp, err := srv.Transform(context.Background(), &pb.TransformRequest{Payload: []byte(raw)})
	if err != nil {
		t.Fatalf("transform returned error: %v", err)
	}
	if resp.GetStatus() != pb.Status_OK {
		t.Fatalf("unexpected status: %v", resp.GetStatus())
	}
	if len(resp.GetEvents()) != 1 {
		t.Fatalf("expected 1 event, got %d", len(resp.GetEvents()))
	}

	ev := resp.GetEvents()[0]
	if ev.Metadata == nil {
		t.Fatalf("metadata missing")
	}

	if got := ev.Metadata.GetAttributes()["sink.key"]; got != "evt-000001-def456" {
		t.Fatalf("unexpected sink.key: %q", got)
	}
	if got := ev.Metadata.GetHeaders()["provider"]; got != "ANTHROPIC" {
		t.Fatalf("unexpected provider header: %q", got)
	}
	if got := ev.Metadata.GetHeaders()["status"]; got != "SUCCESS" {
		t.Fatalf("unexpected status header: %q", got)
	}
	if got := ev.Metadata.GetHeaders()["status-class"]; got != "success" {
		t.Fatalf("unexpected status-class header: %q", got)
	}

	var out normalizedEvent
	if err := json.Unmarshal(ev.GetValue(), &out); err != nil {
		t.Fatalf("failed to decode normalized payload: %v", err)
	}
	if out.EventID != "evt-000001-def456" {
		t.Fatalf("unexpected event_id: %s", out.EventID)
	}
	if out.Provider != "ANTHROPIC" {
		t.Fatalf("provider not uppercased: %s", out.Provider)
	}
	if out.Model != "claude-opus-4" {
		t.Fatalf("unexpected model: %s", out.Model)
	}
	if out.Usage.TotalTokens != 2300 {
		t.Fatalf("total tokens not computed: %d", out.Usage.TotalTokens)
	}
	if out.Environment != "PRODUCTION" {
		t.Fatalf("environment not uppercased: %s", out.Environment)
	}
	if out.Params.FinishReason != "STOP" {
		t.Fatalf("finish_reason not uppercased: %s", out.Params.FinishReason)
	}
}
