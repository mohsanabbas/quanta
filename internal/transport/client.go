package transport

import (
	"fmt"

	pb "quanta/api/proto/v1"
	qerr "quanta/internal/errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func Dial(port int) (pb.ControlClient, error) {
	cc, err := grpc.NewClient(
		fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, qerr.Transport("control", "dial", err)
	}
	return pb.NewControlClient(cc), nil
}
