package transport

import (
	"fmt"

	pb "quanta/api/proto/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func Dial(port int) (pb.ControlClient, error) {
	cc, err := grpc.NewClient(
		fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	return pb.NewControlClient(cc), nil
}
