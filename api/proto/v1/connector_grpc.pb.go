package pb

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const _ = grpc.SupportPackageIsVersion9

const (
	Connector_Stream_FullMethodName = "/quanta.v1.Connector/Stream"
)

type ConnectorClient interface {
	Stream(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[Frame, ConnectorAck], error)
}

type connectorClient struct {
	cc grpc.ClientConnInterface
}

func NewConnectorClient(cc grpc.ClientConnInterface) ConnectorClient {
	return &connectorClient{cc}
}

func (c *connectorClient) Stream(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[Frame, ConnectorAck], error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	stream, err := c.cc.NewStream(ctx, &Connector_ServiceDesc.Streams[0], Connector_Stream_FullMethodName, cOpts...)
	if err != nil {
		return nil, err
	}
	x := &grpc.GenericClientStream[Frame, ConnectorAck]{ClientStream: stream}
	return x, nil
}

type Connector_StreamClient = grpc.BidiStreamingClient[Frame, ConnectorAck]

type ConnectorServer interface {
	Stream(grpc.BidiStreamingServer[Frame, ConnectorAck]) error
	mustEmbedUnimplementedConnectorServer()
}

type UnimplementedConnectorServer struct{}

func (UnimplementedConnectorServer) Stream(grpc.BidiStreamingServer[Frame, ConnectorAck]) error {
	return status.Errorf(codes.Unimplemented, "method Stream not implemented")
}
func (UnimplementedConnectorServer) mustEmbedUnimplementedConnectorServer() {}
func (UnimplementedConnectorServer) testEmbeddedByValue()                   {}

type UnsafeConnectorServer interface {
	mustEmbedUnimplementedConnectorServer()
}

func RegisterConnectorServer(s grpc.ServiceRegistrar, srv ConnectorServer) {

	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&Connector_ServiceDesc, srv)
}

func _Connector_Stream_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(ConnectorServer).Stream(&grpc.GenericServerStream[Frame, ConnectorAck]{ServerStream: stream})
}

type Connector_StreamServer = grpc.BidiStreamingServer[Frame, ConnectorAck]

var Connector_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "quanta.v1.Connector",
	HandlerType: (*ConnectorServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Stream",
			Handler:       _Connector_Stream_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "v1/connector.proto",
}
