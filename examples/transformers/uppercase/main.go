package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"os"
	"strings"
	"time"

	pb "quanta/api/proto/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type rawEvent struct {
	Properties struct {
		CorrelationID      string `json:"correlation_id"`
		CardHash           string `json:"card_hash"`
		CardBIN            string `json:"card_bin"`
		CardLastFour       string `json:"card_last_four"`
		CardExpirationDate string `json:"card_expiration_date"`
		Status             string `json:"status"`
		DeviceAppVersion   string `json:"device_app_version"`
		Origin             string `json:"origin"`
		DeviceID           string `json:"device_id"`
		DeviceOS           string `json:"device_os"`
		DeviceModel        string `json:"device_model"`
		DeviceSessionID    string `json:"device_session_id"`
		IsThirdParty       bool   `json:"is_third_party"`
	} `json:"properties"`
	Context struct {
		EventContractID string `json:"event_contract_id"`
		Event           string `json:"event"`
		AppName         string `json:"app_name"`
		AppVersion      string `json:"app_version"`
		AppType         string `json:"app_type"`
		CreatedAt       string `json:"created_at"`
		UserID          string `json:"user_id"`
		UserType        string `json:"user_type"`
	} `json:"context"`
	Custom any `json:"custom"`
}

type normalizedEvent struct {
	EventID       string    `json:"event_id"`
	EventName     string    `json:"event_name"`
	EventStatus   string    `json:"event_status"`
	StatusClass   string    `json:"status_class"`
	Origin        string    `json:"origin"`
	OccurredAt    time.Time `json:"occurred_at"`
	CorrelationID string    `json:"correlation_id"`
	Device        device    `json:"device"`
	User          user      `json:"user"`
	Card          card      `json:"card"`
}

type device struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	OS         string `json:"os"`
	AppVersion string `json:"app_version"`
	SessionID  string `json:"session_id"`
}

type user struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type card struct {
	BIN        string `json:"bin"`
	LastFour   string `json:"last_four"`
	Expiration string `json:"expiration"`
	Hash       string `json:"hash"`
}

type transformerServer struct {
	pb.UnimplementedTransformServiceServer
}

func (s *transformerServer) Metadata(context.Context, *pb.MetadataRequest) (*pb.MetadataResponse, error) {
	return &pb.MetadataResponse{
		Name:            "card-registration-normalizer",
		Version:         "1.1.0",
		ProtocolVersion: &pb.PluginVersion{Major: 1, Minor: 0, Patch: 0},
		Capabilities:    map[string]string{"batch": "false"},
	}, nil
}

func (s *transformerServer) Health(context.Context, *pb.HealthRequest) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{Ok: true, Details: "ready"}, nil
}

func (s *transformerServer) TransformStream(pb.TransformService_TransformStreamServer) error {
	return status.Errorf(codes.Unimplemented, "streaming not implemented")
}

func (s *transformerServer) Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
	var in rawEvent
	if err := json.Unmarshal(req.GetPayload(), &in); err != nil {
		return &pb.TransformResponse{Status: pb.Status_DROP, ErrorMessage: err.Error()}, nil
	}

	key := in.Context.EventContractID
	if key == "" {
		key = in.Properties.CorrelationID
	}

	occurredAt, err := time.Parse(time.RFC3339, in.Context.CreatedAt)
	if err != nil {
		occurredAt = time.Now().UTC()
	}

	statusUpper := strings.ToUpper(in.Properties.Status)
	eventName := strings.ToLower(in.Context.Event)
	statusClass := classifyStatus(statusUpper)

	norm := normalizedEvent{
		EventID:       key,
		EventName:     eventName,
		EventStatus:   statusUpper,
		StatusClass:   statusClass,
		Origin:        in.Properties.Origin,
		OccurredAt:    occurredAt,
		CorrelationID: in.Properties.CorrelationID,
		Device: device{
			ID:         in.Properties.DeviceID,
			Model:      in.Properties.DeviceModel,
			OS:         strings.ToUpper(in.Properties.DeviceOS),
			AppVersion: in.Properties.DeviceAppVersion,
			SessionID:  in.Properties.DeviceSessionID,
		},
		User: user{
			ID:   in.Context.UserID,
			Type: in.Context.UserType,
		},
		Card: card{
			BIN:        in.Properties.CardBIN,
			LastFour:   in.Properties.CardLastFour,
			Expiration: in.Properties.CardExpirationDate,
			Hash:       in.Properties.CardHash,
		},
	}

	payload, err := json.Marshal(norm)
	if err != nil {
		return &pb.TransformResponse{Status: pb.Status_DROP, ErrorMessage: err.Error()}, nil
	}

	headers := map[string]string{
		"event-name":   norm.EventName,
		"status":       norm.EventStatus,
		"status-class": norm.StatusClass,
	}

	meta := &pb.EventMetadata{
		TimestampMs: time.Now().UnixMilli(),
		Headers:     headers,
		Attributes: map[string]string{
			"sink.key": norm.EventID,
		},
	}

	ev := &pb.Event{Value: payload, Metadata: meta}
	return &pb.TransformResponse{Status: pb.Status_OK, Events: []*pb.Event{ev}}, nil
}

func classifyStatus(status string) string {
	switch {
	case strings.Contains(status, "APPROV"), strings.Contains(status, "UNRESTRICT"):
		return "approved"
	case strings.Contains(status, "REJECT"):
		return "rejected"
	case strings.Contains(status, "PEND"):
		return "pending"
	default:
		return "unknown"
	}
}

func main() {
	listen := flag.String("listen", os.Getenv("TRANSFORMER_LISTEN"), "listen address")
	flag.Parse()
	addr := *listen
	if addr == "" {
		addr = ":50052"
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	pb.RegisterTransformServiceServer(srv, &transformerServer{})
	log.Printf("transformer listening on %s", addr)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
