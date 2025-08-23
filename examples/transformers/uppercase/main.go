package main

import (
	"context"
	"encoding/json"
	"flag"
	"net"
	pb "quanta/api/proto/v1"
	"quanta/internal/logging"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UppercasePlugin struct {
	pb.UnimplementedTransformServiceServer
}

func (p *UppercasePlugin) Metadata(ctx context.Context, _ *pb.MetadataRequest) (*pb.MetadataResponse, error) {
	return &pb.MetadataResponse{
		Name:            "uppercase",
		Version:         "0.1.0",
		ProtocolVersion: &pb.PluginVersion{Major: 1, Minor: 0, Patch: 0},
		Capabilities:    map[string]string{"batch": "false"},
	}, nil
}

func (p *UppercasePlugin) Health(ctx context.Context, _ *pb.HealthRequest) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{Ok: true, Details: "OK"}, nil
}

func (p *UppercasePlugin) TransformStream(pb.TransformService_TransformStreamServer) error {
	return status.Errorf(codes.Unimplemented, "streaming not implemented")
}

func main() {
	listenAddr := flag.String("listen", ":50052", "address to listen on")
	flag.Parse()

	lis, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		logging.L().Error("uppercase: failed to listen", "err", err)
		return
	}
	s := grpc.NewServer()
	pb.RegisterTransformServiceServer(s, &UppercasePlugin{})
	logging.L().Info("uppercase plugin listening", "addr", *listenAddr)
	if err := s.Serve(lis); err != nil {
		logging.L().Error("uppercase: failed to serve", "err", err)
	}
}

// A small struct to pull an event name for logging if present
// { "context": { "event": "..." } }
type eventWrapper struct {
	Context struct {
		Event string `json:"event"`
	} `json:"context"`
}

func (p *UppercasePlugin) Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
	// Try to log the event name if the payload looks like Amplitude-style JSON
	var wrapper eventWrapper
	if err := json.Unmarshal(req.Payload, &wrapper); err == nil && wrapper.Context.Event != "" {
		logging.L().Info("uppercase received event", "event", wrapper.Context.Event)
	}

	out := req.Payload
	// If JSON, add a marker field; otherwise uppercase bytes
	var obj map[string]any
	if err := json.Unmarshal(req.Payload, &obj); err == nil {
		obj["_transformed"] = "uppercase"
		if b, err := json.Marshal(obj); err == nil {
			out = b
		}
	} else {
		out = []byte(strings.ToUpper(string(req.Payload)))
	}

	ev := &pb.Event{
		Id:       req.Metadata.SourceOffset,
		Value:    out,
		Metadata: req.Metadata,
	}
	if ev.Metadata == nil {
		ev.Metadata = &pb.EventMetadata{}
	}
	if ev.Metadata.Attributes == nil {
		ev.Metadata.Attributes = map[string]string{}
	}
	ev.Metadata.Attributes["transformed_by"] = "uppercase"

	return &pb.TransformResponse{
		Events: []*pb.Event{ev},
		Status: pb.Status_OK,
	}, nil
}

//go build -o uppercase ./quanta/examples/transformers/uppercase
//./uppercase --listen=:50051
