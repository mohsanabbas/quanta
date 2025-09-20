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
            "correlation_id": "0c710a15-54c6-4718-b1ac-76e3b60acba7",
            "card_hash": "510bb235d416da4befb36b62b1b69695fbad86a8",
            "card_bin": "511684",
            "card_last_four": "9255",
            "card_expiration_date": "12/2027",
            "status": "UNRESTRICTED",
            "device_app_version": "11.0.142",
            "origin": "new_wallet",
            "device_id": "fa0222c1-d91e-47f7-b94f-e522ff5ad12c",
            "device_os": "android",
            "device_model": "galaxy",
            "device_session_id": "35ebb255-902a-427c-8f31-6b8c5fbe7025",
            "is_third_party": true
        },
        "context": {
            "event_contract_id": "8996213a-5800-4e28-966e-a07c097c36b6",
            "event": "request_card_registration_status_changed",
            "app_name": "ms-credit-cards-registration",
            "app_version": "7c4d9e6591781e954689ee0069f4856bfc86cc28",
            "app_type": "BACKEND",
            "created_at": "2024-11-12T09:37:05-03:00",
            "user_id": "26232789",
            "user_type": "CONSUMER"
        },
        "custom": null
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

	if got := ev.Metadata.GetAttributes()["sink.key"]; got != "8996213a-5800-4e28-966e-a07c097c36b6" {
		t.Fatalf("unexpected sink.key: %q", got)
	}
	if got := ev.Metadata.GetHeaders()["status"]; got != "UNRESTRICTED" {
		t.Fatalf("unexpected status header: %q", got)
	}
	if got := ev.Metadata.GetHeaders()["status-class"]; got != "approved" {
		t.Fatalf("unexpected status-class header: %q", got)
	}

	var out normalizedEvent
	if err := json.Unmarshal(ev.GetValue(), &out); err != nil {
		t.Fatalf("failed to decode normalized payload: %v", err)
	}
	if out.EventID != "8996213a-5800-4e28-966e-a07c097c36b6" {
		t.Fatalf("unexpected event_id: %s", out.EventID)
	}
	if out.Device.OS != "ANDROID" {
		t.Fatalf("device os normalization failed: %s", out.Device.OS)
	}
	if out.Card.BIN != "511684" || out.Card.LastFour != "9255" {
		t.Fatalf("card fields not carried forward: %+v", out.Card)
	}
}
