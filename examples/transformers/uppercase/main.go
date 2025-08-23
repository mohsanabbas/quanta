package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	pb "quanta/api/proto/v1"

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

//func (p *UppercasePlugin) Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
//	out := strings.ToUpper(string(req.Payload))
//	ev := &pb.Event{
//		Id:       req.Metadata.SourceOffset, // use offset as id in this example
//		Value:    []byte(out),
//		Metadata: req.Metadata,
//	}
//	return &pb.TransformResponse{
//		Events: []*pb.Event{ev},
//		Status: pb.Status_OK,
//	}, nil
//}

func (p *UppercasePlugin) TransformStream(pb.TransformService_TransformStreamServer) error {
	return status.Errorf(codes.Unimplemented, "streaming not implemented")
}

func main() {
	listenAddr := flag.String("listen", ":50051", "address to listen on")
	flag.Parse()

	lis, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterTransformServiceServer(s, &UppercasePlugin{})
	log.Printf("uppercase plugin listening on %s", *listenAddr)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

// define only the parts of the JSON you care about
type eventWrapper struct {
	Context struct {
		Event string `json:"event"`
	} `json:"context"`
}

func (p *UppercasePlugin) Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
	// Unmarshal the payload into the wrapper
	var wrapper eventWrapper
	if err := json.Unmarshal(req.Payload, &wrapper); err != nil {
		// if the payload isn’t valid JSON, return an error response
		return &pb.TransformResponse{
			Status:       pb.Status_ERROR,
			ErrorMessage: err.Error(),
		}, nil
	}

	// You can now access wrapper.Context.Event
	eventName := wrapper.Context.Event
	log.Printf("received event %s", eventName)

	// Do whatever transformation you need, e.g. include the event name in output
	// or route/filter based on it. Here we’ll just return the original payload.
	ev := &pb.Event{
		Id:       req.Metadata.SourceOffset,
		Value:    req.Payload, // unchanged
		Metadata: req.Metadata,
	}
	// you could also add the event name to metadata.Attributes if useful:
	if ev.Metadata == nil {
		ev.Metadata = &pb.EventMetadata{}
	}
	if ev.Metadata.Attributes == nil {
		ev.Metadata.Attributes = map[string]string{}
	}
	ev.Metadata.Attributes["event_name"] = eventName

	return &pb.TransformResponse{
		Events: []*pb.Event{ev},
		Status: pb.Status_OK,
	}, nil
}

//go build -o uppercase ./quanta/examples/transformers/uppercase
//./uppercase --listen=:50051
