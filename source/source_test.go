package source

import (
	"context"
	"errors"
	"testing"

	pb "quanta/api/proto/v1"
)

func cleanRegistry(t *testing.T) {
	t.Helper()
	saved := _registry
	_registry = map[string]Registration{}
	t.Cleanup(func() { _registry = saved })
}

type stubAdapter struct{}

func (stubAdapter) Configure(context.Context, any) error  { return nil }
func (stubAdapter) Run(context.Context, EmitFunc) error   { return nil }
func (stubAdapter) OnAck(_ *pb.ConnectorAck)              {}
func (stubAdapter) Close(context.Context) error           { return nil }

func TestRegister_Lookup_Found(t *testing.T) {
	cleanRegistry(t)

	Register(Registration{
		Name: "test-source",
		New:  func() Adapter { return stubAdapter{} },
	})

	reg, ok := Lookup("test-source")
	if !ok {
		t.Fatal("Lookup: expected ok=true for registered source")
	}
	if reg.Name != "test-source" {
		t.Fatalf("Lookup name: got %q, want %q", reg.Name, "test-source")
	}
}

func TestRegister_Lookup_NotFound(t *testing.T) {
	cleanRegistry(t)

	_, ok := Lookup("nonexistent")
	if ok {
		t.Fatal("Lookup: expected ok=false for unregistered source")
	}
}

func TestRegister_PanicsOnEmptyName(t *testing.T) {
	cleanRegistry(t)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register with empty name must panic")
		}
	}()

	Register(Registration{
		Name: "",
		New:  func() Adapter { return stubAdapter{} },
	})
}

func TestRegister_PanicsOnNilConstructor(t *testing.T) {
	cleanRegistry(t)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register with nil New must panic")
		}
	}()

	Register(Registration{Name: "x", New: nil})
}

func TestNew_KnownAdapter(t *testing.T) {
	cleanRegistry(t)

	Register(Registration{
		Name: "stub",
		New:  func() Adapter { return stubAdapter{} },
	})

	a, err := New("stub")
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("New: returned nil adapter")
	}
}

func TestNew_UnknownAdapter(t *testing.T) {
	cleanRegistry(t)

	_, err := New("unknown")
	if err == nil {
		t.Fatal("New: expected error for unknown adapter")
	}
}

func TestLoadConfig_NoLoaderRegistered(t *testing.T) {
	cleanRegistry(t)

	Register(Registration{
		Name:       "no-loader",
		New:        func() Adapter { return stubAdapter{} },
		LoadConfig: nil,
	})

	_, err := LoadConfig("no-loader", "config.yaml")
	if err == nil {
		t.Fatal("LoadConfig: expected error when no loader registered")
	}
}

func TestLoadConfig_UnknownSource(t *testing.T) {
	cleanRegistry(t)

	_, err := LoadConfig("missing", "config.yaml")
	if err == nil {
		t.Fatal("LoadConfig: expected error for unknown source")
	}
}

func TestLoadConfig_InvokesLoader(t *testing.T) {
	cleanRegistry(t)

	Register(Registration{
		Name: "with-loader",
		New:  func() Adapter { return stubAdapter{} },
		LoadConfig: func(path string) (any, error) {
			if path != "my.yaml" {
				return nil, errors.New("unexpected path: " + path)
			}
			return "loaded-config", nil
		},
	})

	got, err := LoadConfig("with-loader", "my.yaml")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got != "loaded-config" {
		t.Fatalf("LoadConfig result: got %v, want %q", got, "loaded-config")
	}
}

func TestRegister_OverwritesPreviousRegistration(t *testing.T) {
	cleanRegistry(t)

	Register(Registration{Name: "dup", New: func() Adapter { return stubAdapter{} }})
	Register(Registration{Name: "dup", New: func() Adapter { return stubAdapter{} }})

	_, ok := Lookup("dup")
	if !ok {
		t.Fatal("Lookup after re-register: expected ok=true")
	}
}
