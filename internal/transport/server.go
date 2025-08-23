package transport

import (
	"fmt"
	"net"

	pb "quanta/api/proto/v1"

	"google.golang.org/grpc"
)

type Server struct {
	grpc *grpc.Server
	lis  net.Listener
}

func StartServer(port int) (*Server, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	s := &Server{
		grpc: grpc.NewServer(),
		lis:  lis,
	}
	// register empty services; real impl later
	//pb.RegisterConnectorServer(s.grpc, UnimplementedConnector{})
	pb.RegisterControlServer(s.grpc, UnimplementedControl{})
	return s, nil
}

func (s *Server) Serve() error {
	return s.grpc.Serve(s.lis)
}
func (s *Server) Stop() {
	s.grpc.GracefulStop()
}

// ----- stubs --------------------------------------------------------------

type UnimplementedConnector struct {
	pb.UnimplementedConnectorServer
}
type UnimplementedControl struct {
	pb.UnimplementedControlServer
}
