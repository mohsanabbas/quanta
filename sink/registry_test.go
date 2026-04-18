package sink

import (
	"context"
	"errors"
	"testing"

	pb "quanta/api/proto/v1"
)

// fakeAdapter is a minimal Adapter used to exercise registry plumbing.
type fakeAdapter struct {
	caps Capabilities
	opts BuildOptions
}

func (f *fakeAdapter) Name() string                             { return "fake" }
func (f *fakeAdapter) Caps() Capabilities                       { return f.caps }
func (f *fakeAdapter) Publish(context.Context, *pb.Frame) error { return nil }
func (f *fakeAdapter) Close(context.Context) error              { return nil }

func TestRegister_PanicsOnMissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		give Registration
	}{
		{name: "no_name", give: Registration{
			DecodeConfig: func(any) (any, error) { return nil, nil },
			New:          func(context.Context, any, BuildOptions) (Adapter, error) { return nil, nil },
		}},
		{name: "no_factory", give: Registration{
			Name:         "x",
			DecodeConfig: func(any) (any, error) { return nil, nil },
		}},
		{name: "no_decode", give: Registration{
			Name: "y",
			New:  func(context.Context, any, BuildOptions) (Adapter, error) { return nil, nil },
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("Register did not panic on invalid registration")
				}
			}()
			Register(tt.give)
		})
	}
}

func TestBuild_UnknownAdapter(t *testing.T) {
	t.Parallel()

	_, err := Build(context.Background(), "nonexistent-driver-xyzzy", nil, BuildOptions{})
	if err == nil {
		t.Fatal("Build with unknown name must return error")
	}
}

func TestBuild_DecodeError_AttributedToDriver(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("bad config")
	Register(Registration{
		Name:         "decode-fail-driver",
		DecodeConfig: func(any) (any, error) { return nil, wantErr },
		New:          func(context.Context, any, BuildOptions) (Adapter, error) { return nil, nil },
	})

	_, err := Build(context.Background(), "decode-fail-driver", nil, BuildOptions{})
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("Build error: got %v, want chain containing %v", err, wantErr)
	}
}

func TestBuild_PassesOptionsToFactory(t *testing.T) {
	t.Parallel()

	var gotOpts BuildOptions
	Register(Registration{
		Name:         "opts-capture-driver",
		DecodeConfig: func(any) (any, error) { return nil, nil },
		New: func(_ context.Context, _ any, opts BuildOptions) (Adapter, error) {
			gotOpts = opts
			return &fakeAdapter{caps: Capabilities{AckAware: true}, opts: opts}, nil
		},
	})

	wantAck := func(context.Context, *pb.CheckpointToken) {}
	a, err := Build(context.Background(), "opts-capture-driver", nil, BuildOptions{Ack: wantAck})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if gotOpts.Ack == nil {
		t.Fatal("BuildOptions.Ack not propagated to factory")
	}
	if !a.Caps().AckAware {
		t.Fatal("Caps().AckAware: got false, want true")
	}
}
