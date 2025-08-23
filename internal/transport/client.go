package transport

import (
	"fmt"

	pb "quanta/api/proto/v1"

	"google.golang.org/grpc"
)

func Dial(port int) (pb.ControlClient, error) {
	cc, err := grpc.Dial(fmt.Sprintf("localhost:%d", port), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	return pb.NewControlClient(cc), nil
}
