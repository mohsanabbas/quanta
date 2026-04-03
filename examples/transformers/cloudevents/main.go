package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	pb "quanta/api/proto/v1"

	cloudevents "github.com/cloudevents/sdk-go/v2/event"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// eventData is the domain payload carried inside the CloudEvent data field.
type eventData struct {
	RequestID    string  `json:"request_id"`
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	Status       string  `json:"status"`
	StatusClass  string  `json:"status_class"`
	Origin       string  `json:"origin"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	LatencyMs    int     `json:"latency_ms"`
	Temperature  float64 `json:"temperature"`
	MaxTokens    int     `json:"max_tokens"`
	Stream       bool    `json:"stream"`
	FinishReason string  `json:"finish_reason"`
	UserID       string  `json:"user_id"`
	OrgID        string  `json:"org_id"`
	AppName      string  `json:"app_name"`
	AppVersion   string  `json:"app_version"`
	Environment  string  `json:"environment"`
}

// dlqEnvelope wraps an unprocessable raw event with error context.
type dlqEnvelope struct {
	Error       string          `json:"error"`
	ErrorClass  string          `json:"error_class"`
	Transformer string          `json:"transformer"`
	ReceivedAt  string          `json:"received_at"`
	RawPayload  json.RawMessage `json:"raw_payload"`
}

// rawEvent mirrors the seed producer schema.
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

const (
	_sourceBase  = "/quanta/ai"
	_typePrefix  = "ai.quanta."
	_dlqTopic    = "quanta-dlq"
	_outputTopic = "quanta-output"
	_pluginName  = "cloudevents-normalizer"
)

type transformerServer struct {
	pb.UnimplementedTransformServiceServer
}

func (s *transformerServer) Metadata(context.Context, *pb.MetadataRequest) (*pb.MetadataResponse, error) {
	return &pb.MetadataResponse{
		Name:            _pluginName,
		Version:         "1.0.0",
		ProtocolVersion: &pb.PluginVersion{Major: 1, Minor: 0, Patch: 0},
		Capabilities:    map[string]string{"batch": "false", "spec": "cloudevents/1.0"},
	}, nil
}

func (s *transformerServer) Health(context.Context, *pb.HealthRequest) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{Ok: true, Details: "ready"}, nil
}

func (s *transformerServer) TransformStream(pb.TransformService_TransformStreamServer) error {
	return status.Errorf(codes.Unimplemented, "streaming not implemented")
}

func (s *transformerServer) Transform(_ context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
	var in rawEvent
	if err := json.Unmarshal(req.GetPayload(), &in); err != nil {
		return s.toDLQ(req.GetPayload(), "unmarshal_error", err.Error()), nil
	}

	if in.Properties.Provider == "" || in.Context.Event == "" {
		return s.toDLQ(req.GetPayload(), "validation_error", "missing required field: provider or event"), nil
	}

	ceID := in.Context.EventContractID
	if ceID == "" {
		ceID = in.Properties.RequestID
	}
	if ceID == "" {
		return s.toDLQ(req.GetPayload(), "validation_error", "missing event id"), nil
	}

	occurredAt, err := time.Parse(time.RFC3339, in.Context.CreatedAt)
	if err != nil {
		occurredAt = time.Now().UTC()
	}

	statusClass := classifyStatus(strings.ToUpper(in.Properties.Status))
	eventName := strings.ToLower(in.Context.Event)

	// Route unhealthy events to DLQ — errors, timeouts, and rate-limited
	// requests don't belong in the clean output topic.
	if statusClass == "error" || statusClass == "throttled" {
		reason := fmt.Sprintf("unhealthy event: status=%s class=%s", in.Properties.Status, statusClass)
		return s.toDLQ(req.GetPayload(), "status_rejected", reason), nil
	}

	// Build CloudEvent using the official SDK.
	ce := cloudevents.New()
	ce.SetID(ceID)
	ce.SetType(_typePrefix + eventName)
	ce.SetSource(fmt.Sprintf("%s/%s/%s", _sourceBase, in.Properties.Provider, in.Context.AppName))
	ce.SetSubject(in.Properties.Model)
	ce.SetTime(occurredAt)

	// Extension attributes (lowercase, ≤20 chars per spec).
	ce.SetExtension("aiprovider", strings.ToLower(in.Properties.Provider))
	ce.SetExtension("environment", strings.ToLower(in.Context.Environment))
	ce.SetExtension("statusclass", statusClass)

	data := eventData{
		RequestID:    in.Properties.RequestID,
		Provider:     strings.ToLower(in.Properties.Provider),
		Model:        in.Properties.Model,
		Status:       strings.ToLower(in.Properties.Status),
		StatusClass:  statusClass,
		Origin:       in.Properties.Origin,
		InputTokens:  in.Properties.InputTokens,
		OutputTokens: in.Properties.OutputTokens,
		TotalTokens:  in.Properties.InputTokens + in.Properties.OutputTokens,
		LatencyMs:    in.Properties.LatencyMs,
		Temperature:  in.Properties.Temperature,
		MaxTokens:    in.Properties.MaxTokens,
		Stream:       in.Properties.Stream,
		FinishReason: strings.ToLower(in.Properties.FinishReason),
		UserID:       in.Context.UserID,
		OrgID:        in.Context.OrgID,
		AppName:      in.Context.AppName,
		AppVersion:   in.Context.AppVersion,
		Environment:  strings.ToLower(in.Context.Environment),
	}

	if err := ce.SetData("application/json", data); err != nil {
		return s.toDLQ(req.GetPayload(), "marshal_error", err.Error()), nil
	}

	payload, err := json.Marshal(ce)
	if err != nil {
		return s.toDLQ(req.GetPayload(), "marshal_error", err.Error()), nil
	}

	// CloudEvents Kafka protocol binding headers (ce- prefix).
	headers := map[string]string{
		"ce-specversion": ce.SpecVersion(),
		"ce-type":        ce.Type(),
		"ce-source":      ce.Source(),
		"ce-id":          ce.ID(),
		"ce-time":        ce.Time().Format(time.RFC3339),
		"ce-subject":     ce.Subject(),
		"ce-aiprovider":  ce.Extensions()["aiprovider"].(string),
		"ce-environment": ce.Extensions()["environment"].(string),
		"ce-statusclass": ce.Extensions()["statusclass"].(string),
		"__topic":        _outputTopic,
	}

	meta := &pb.EventMetadata{
		TimestampMs: occurredAt.UnixMilli(),
		Headers:     headers,
		Attributes: map[string]string{
			"sink.key": ceID,
		},
	}

	return &pb.TransformResponse{
		Status: pb.Status_OK,
		Events: []*pb.Event{{Value: payload, Metadata: meta}},
	}, nil
}

func (s *transformerServer) toDLQ(raw []byte, errClass, errMsg string) *pb.TransformResponse {
	// If raw is valid JSON, embed it directly; otherwise base64-encode.
	var rawPayload json.RawMessage
	if json.Valid(raw) {
		rawPayload = raw
	} else {
		quoted, _ := json.Marshal(base64.StdEncoding.EncodeToString(raw))
		rawPayload = quoted
	}

	envelope := dlqEnvelope{
		Error:       errMsg,
		ErrorClass:  errClass,
		Transformer: _pluginName,
		ReceivedAt:  time.Now().UTC().Format(time.RFC3339),
		RawPayload:  rawPayload,
	}

	payload, err := json.Marshal(envelope)
	if err != nil {
		payload = raw
	}

	headers := map[string]string{
		"dlq-error":       errMsg,
		"dlq-error-class": errClass,
		"dlq-transformer": _pluginName,
		"__topic":         _dlqTopic,
	}

	meta := &pb.EventMetadata{
		TimestampMs: time.Now().UnixMilli(),
		Headers:     headers,
	}

	return &pb.TransformResponse{
		Status: pb.Status_OK,
		Events: []*pb.Event{{Value: payload, Metadata: meta}},
	}
}

func classifyStatus(s string) string {
	switch s {
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
	log.Printf("[%s] listening on %s", _pluginName, lis.Addr())

	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
