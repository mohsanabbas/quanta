package transport

import (
	"fmt"
	"net"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"

	"google.golang.org/grpc"
)

type Server struct {
	grpc *grpc.Server
	lis  net.Listener
}

func StartServer(port int) (*Server, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, qerr.Transport("grpc", "listen", err)
	}
	s := &Server{
		grpc: grpc.NewServer(),
		lis:  lis,
	}

	pb.RegisterControlServer(s.grpc, &pb.UnimplementedControlServer{})
	pb.RegisterHealthServer(s.grpc, &pb.UnimplementedHealthServer{})

	return s, nil
}

func (s *Server) Serve() error {
	return s.grpc.Serve(s.lis)
}

func (s *Server) Stop() {
	s.grpc.GracefulStop()
}
