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
		RequestID    string  `json:"request_id"`
		Provider     string  `json:"provider"`
		Model        string  `json:"model"`
		Status       string  `json:"status"`
		InputTokens  int     `json:"input_tokens"`
		OutputTokens int     `json:"output_tokens"`
		LatencyMs    int     `json:"latency_ms"`
		Temperature  float64 `json:"temperature"`
		MaxTokens    int     `json:"max_tokens"`
		Stream       bool    `json:"stream"`
		FinishReason string  `json:"finish_reason"`
		Origin       string  `json:"origin"`
	} `json:"properties"`
	Context struct {
		EventContractID string `json:"event_contract_id"`
		Event           string `json:"event"`
		AppName         string `json:"app_name"`
		AppVersion      string `json:"app_version"`
		CreatedAt       string `json:"created_at"`
		UserID          string `json:"user_id"`
		OrgID           string `json:"org_id"`
		Environment     string `json:"environment"`
	} `json:"context"`
}

type normalizedEvent struct {
	EventID     string    `json:"event_id"`
	EventName   string    `json:"event_name"`
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	Status      string    `json:"status"`
	StatusClass string    `json:"status_class"`
	Origin      string    `json:"origin"`
	OccurredAt  time.Time `json:"occurred_at"`
	RequestID   string    `json:"request_id"`
	Usage       usage     `json:"usage"`
	Params      params    `json:"params"`
	User        user      `json:"user"`
	Environment string    `json:"environment"`
}

type usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
	LatencyMs    int `json:"latency_ms"`
}

type params struct {
	Temperature  float64 `json:"temperature"`
	MaxTokens    int     `json:"max_tokens"`
	Stream       bool    `json:"stream"`
	FinishReason string  `json:"finish_reason"`
}

type user struct {
	ID    string `json:"id"`
	OrgID string `json:"org_id"`
}

type transformerServer struct {
	pb.UnimplementedTransformServiceServer
}

func (s *transformerServer) Metadata(context.Context, *pb.MetadataRequest) (*pb.MetadataResponse, error) {
	return &pb.MetadataResponse{
		Name:            "ai-event-normalizer",
		Version:         "2.0.0",
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
		key = in.Properties.RequestID
	}

	occurredAt, err := time.Parse(time.RFC3339, in.Context.CreatedAt)
	if err != nil {
		occurredAt = time.Now().UTC()
	}

	statusUpper := strings.ToUpper(in.Properties.Status)
	providerUpper := strings.ToUpper(in.Properties.Provider)
	eventName := strings.ToLower(in.Context.Event)
	statusClass := classifyStatus(statusUpper)

	norm := normalizedEvent{
		EventID:     key,
		EventName:   eventName,
		Provider:    providerUpper,
		Model:       in.Properties.Model,
		Status:      statusUpper,
		StatusClass: statusClass,
		Origin:      in.Properties.Origin,
		OccurredAt:  occurredAt,
		RequestID:   in.Properties.RequestID,
		Usage: usage{
			InputTokens:  in.Properties.InputTokens,
			OutputTokens: in.Properties.OutputTokens,
			TotalTokens:  in.Properties.InputTokens + in.Properties.OutputTokens,
			LatencyMs:    in.Properties.LatencyMs,
		},
		Params: params{
			Temperature:  in.Properties.Temperature,
			MaxTokens:    in.Properties.MaxTokens,
			Stream:       in.Properties.Stream,
			FinishReason: strings.ToUpper(in.Properties.FinishReason),
		},
		User: user{
			ID:    in.Context.UserID,
			OrgID: in.Context.OrgID,
		},
		Environment: strings.ToUpper(in.Context.Environment),
	}

	payload, err := json.Marshal(norm)
	if err != nil {
		return &pb.TransformResponse{Status: pb.Status_DROP, ErrorMessage: err.Error()}, nil
	}

	headers := map[string]string{
		"event-name":   norm.EventName,
		"provider":     norm.Provider,
		"status":       norm.Status,
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
	switch status {
	case "SUCCESS":
		return "success"
	case "ERROR":
		return "error"
	case "RATE_LIMITED":
		return "throttled"
	case "TIMEOUT":
		return "error"
	case "CONTENT_FILTERED":
		return "filtered"
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
