package pb

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const _ = grpc.SupportPackageIsVersion9

const (
	Control_Ping_FullMethodName           = "/quanta.v1.Control/Ping"
	Control_DeployPipeline_FullMethodName = "/quanta.v1.Control/DeployPipeline"
	Control_PausePipeline_FullMethodName  = "/quanta.v1.Control/PausePipeline"
)

type ControlClient interface {
	Ping(ctx context.Context, in *PingRequest, opts ...grpc.CallOption) (*PingReply, error)
	DeployPipeline(ctx context.Context, in *DeployRequest, opts ...grpc.CallOption) (*DeployReply, error)
	PausePipeline(ctx context.Context, in *PauseRequest, opts ...grpc.CallOption) (*PauseReply, error)
}

type controlClient struct {
	cc grpc.ClientConnInterface
}

func NewControlClient(cc grpc.ClientConnInterface) ControlClient {
	return &controlClient{cc}
}

func (c *controlClient) Ping(ctx context.Context, in *PingRequest, opts ...grpc.CallOption) (*PingReply, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(PingReply)
	err := c.cc.Invoke(ctx, Control_Ping_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *controlClient) DeployPipeline(ctx context.Context, in *DeployRequest, opts ...grpc.CallOption) (*DeployReply, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(DeployReply)
	err := c.cc.Invoke(ctx, Control_DeployPipeline_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *controlClient) PausePipeline(ctx context.Context, in *PauseRequest, opts ...grpc.CallOption) (*PauseReply, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(PauseReply)
	err := c.cc.Invoke(ctx, Control_PausePipeline_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type ControlServer interface {
	Ping(context.Context, *PingRequest) (*PingReply, error)
	DeployPipeline(context.Context, *DeployRequest) (*DeployReply, error)
	PausePipeline(context.Context, *PauseRequest) (*PauseReply, error)
	mustEmbedUnimplementedControlServer()
}

type UnimplementedControlServer struct{}

func (UnimplementedControlServer) Ping(context.Context, *PingRequest) (*PingReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Ping not implemented")
}
func (UnimplementedControlServer) DeployPipeline(context.Context, *DeployRequest) (*DeployReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeployPipeline not implemented")
}
func (UnimplementedControlServer) PausePipeline(context.Context, *PauseRequest) (*PauseReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method PausePipeline not implemented")
}
func (UnimplementedControlServer) mustEmbedUnimplementedControlServer() {}
func (UnimplementedControlServer) testEmbeddedByValue()                 {}

type UnsafeControlServer interface {
	mustEmbedUnimplementedControlServer()
}

func RegisterControlServer(s grpc.ServiceRegistrar, srv ControlServer) {

	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&Control_ServiceDesc, srv)
}

func _Control_Ping_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PingRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ControlServer).Ping(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Control_Ping_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ControlServer).Ping(ctx, req.(*PingRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Control_DeployPipeline_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeployRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ControlServer).DeployPipeline(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Control_DeployPipeline_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ControlServer).DeployPipeline(ctx, req.(*DeployRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Control_PausePipeline_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PauseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ControlServer).PausePipeline(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Control_PausePipeline_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ControlServer).PausePipeline(ctx, req.(*PauseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var Control_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "quanta.v1.Control",
	HandlerType: (*ControlServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Ping",
			Handler:    _Control_Ping_Handler,
		},
		{
			MethodName: "DeployPipeline",
			Handler:    _Control_DeployPipeline_Handler,
		},
		{
			MethodName: "PausePipeline",
			Handler:    _Control_PausePipeline_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "v1/control.proto",
}
