package transport

import (
	"fmt"
	"io"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ClientConn struct {
	pb.ControlClient
	conn *grpc.ClientConn
}

var _ io.Closer = (*ClientConn)(nil)

func (c *ClientConn) Close() error {
	return c.conn.Close()
}

func Dial(port int) (*ClientConn, error) {
	cc, err := grpc.NewClient(
		fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, qerr.Transport("control", "dial", err)
	}
	return &ClientConn{
		ControlClient: pb.NewControlClient(cc),
		conn:          cc,
	}, nil
}
