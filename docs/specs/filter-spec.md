# Filter Specification

Filters are a subset of processors that drop frames. In Quanta, a transformer can behave as a filter by returning a `TransformResponse` with status `DROP` and no events.

## Constraints

- Filters must be side-effect free; they should not emit partial events or mutate global state.
- Returning `DROP` signals the runner to acknowledge the frame immediately and stop further processing.
- The pipeline treats `DROP` as a successful terminal state; offsets are committed in all commit modes.

## Implementing a Filter

```go
func (f *Filter) Transform(ctx context.Context, req *pb.TransformRequest) (*pb.TransformResponse, error) {
    if shouldDrop(req) {
        return &pb.TransformResponse{Status: pb.Status_DROP}, nil
    }
    // otherwise pass through
    return &pb.TransformResponse{Status: pb.Status_OK, Events: []*pb.Event{pass(req)}}
}
```

