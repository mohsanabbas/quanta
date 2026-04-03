package errors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindString(t *testing.T) {
	tests := []struct {
		name string
		give Kind
		want string
	}{
		{name: "config", give: KindConfig, want: "config"},
		{name: "transport", give: KindTransport, want: "transport"},
		{name: "source", give: KindSource, want: "source"},
		{name: "transform", give: KindTransform, want: "transform"},
		{name: "sink", give: KindSink, want: "sink"},
		{name: "pipeline", give: KindPipeline, want: "pipeline"},
		{name: "zero", give: Kind(0), want: "unknown"},
		{name: "out_of_range", give: Kind(99), want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.String())
		})
	}
}

func TestErrorMessage(t *testing.T) {
	tests := []struct {
		name     string
		giveKind Kind
		giveComp string
		giveOp   string
		giveErr  error
		wantMsg  string
	}{
		{
			name:     "with_component",
			giveKind: KindSource,
			giveComp: "kafka",
			giveOp:   "configure",
			giveErr:  errors.New("connection refused"),
			wantMsg:  "source[kafka] configure: connection refused",
		},
		{
			name:     "without_component",
			giveKind: KindPipeline,
			giveOp:   "compile",
			giveErr:  errors.New("missing source"),
			wantMsg:  "pipeline compile: missing source",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Error{Kind: tt.giveKind, Component: tt.giveComp, Op: tt.giveOp, Err: tt.giveErr}
			assert.Equal(t, tt.wantMsg, e.Error())
		})
	}
}

func TestErrorUnwrap(t *testing.T) {
	cause := errors.New("root cause")
	e := &Error{Kind: KindConfig, Op: "validate", Err: cause}
	assert.Equal(t, cause, e.Unwrap())
	assert.True(t, errors.Is(e, cause))
}

func TestDomainConstructorsNilSafe(t *testing.T) {
	constructors := []struct {
		name string
		fn   func() error
	}{
		{"Config", func() error { return Config("x", "y", nil) }},
		{"Source", func() error { return Source("x", "y", nil) }},
		{"Sink", func() error { return Sink("x", "y", nil) }},
		{"Transform", func() error { return Transform("x", "y", nil) }},
		{"Transport", func() error { return Transport("x", "y", nil) }},
		{"Pipeline", func() error { return Pipeline("y", nil) }},
	}
	for _, tt := range constructors {
		t.Run(tt.name, func(t *testing.T) {
			assert.Nil(t, tt.fn(), "constructor should return nil for nil error")
		})
	}
}

func TestDomainConstructorsKind(t *testing.T) {
	cause := errors.New("boom")
	tests := []struct {
		name     string
		give     error
		wantKind Kind
		wantComp string
		wantOp   string
	}{
		{"Config", Config("kafka", "validate", cause), KindConfig, "kafka", "validate"},
		{"Source", Source("kafka", "configure", cause), KindSource, "kafka", "configure"},
		{"Sink", Sink("stdout", "publish", cause), KindSink, "stdout", "publish"},
		{"Transform", Transform("uppercase", "dial", cause), KindTransform, "uppercase", "dial"},
		{"Transport", Transport("grpc", "listen", cause), KindTransport, "grpc", "listen"},
		{"Pipeline", Pipeline("compile", cause), KindPipeline, "", "compile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, ok := Extract(tt.give)
			require.True(t, ok, "Extract should find *Error")
			assert.Equal(t, tt.wantKind, e.Kind)
			assert.Equal(t, tt.wantComp, e.Component)
			assert.Equal(t, tt.wantOp, e.Op)
			assert.Equal(t, cause, e.Err)
		})
	}
}

func TestExtractThroughWrapChain(t *testing.T) {
	cause := errors.New("connection refused")
	domainErr := Source("kafka", "dial", cause)

	wrapped := Wrap(domainErr, "pipeline compile")
	doubleWrapped := Wrapf(wrapped, "bootstrap phase %d", 1)

	e, ok := Extract(doubleWrapped)
	require.True(t, ok, "Extract should unwrap through Wrap chain")
	assert.Equal(t, KindSource, e.Kind)
	assert.Equal(t, "kafka", e.Component)
	assert.Equal(t, "dial", e.Op)
	assert.True(t, errors.Is(doubleWrapped, cause))
}

func TestIsKindHelpers(t *testing.T) {
	cause := errors.New("fail")
	tests := []struct {
		name  string
		give  error
		check func(error) bool
		want  bool
	}{
		{"IsConfig_true", Config("x", "y", cause), IsConfig, true},
		{"IsConfig_false", Source("x", "y", cause), IsConfig, false},
		{"IsSource_true", Source("x", "y", cause), IsSource, true},
		{"IsSink_true", Sink("x", "y", cause), IsSink, true},
		{"IsTransform_true", Transform("x", "y", cause), IsTransform, true},
		{"IsTransport_true", Transport("x", "y", cause), IsTransport, true},
		{"IsPipeline_true", Pipeline("y", cause), IsPipeline, true},
		{"nil_error", nil, IsConfig, false},
		{"plain_error", cause, IsConfig, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.check(tt.give))
		})
	}
}

func TestIsKindThroughWrap(t *testing.T) {
	err := Wrap(Config("pipeline", "validate", errors.New("missing field")), "load config")
	assert.True(t, IsConfig(err), "IsConfig should find *Error through Wrap chain")
	assert.False(t, IsSource(err))
}

func TestExtractNonDomainError(t *testing.T) {
	_, ok := Extract(errors.New("plain error"))
	assert.False(t, ok)

	_, ok = Extract(nil)
	assert.False(t, ok)
}

func TestExtractNestedDomainErrors(t *testing.T) {
	inner := Source("kafka", "configure", errors.New("timeout"))
	outer := Pipeline("compile", inner)

	e, ok := Extract(outer)
	require.True(t, ok)
	assert.Equal(t, KindPipeline, e.Kind, "Extract returns outermost *Error")

	inner2, ok := Extract(e.Err)
	require.True(t, ok)
	assert.Equal(t, KindSource, inner2.Kind)
}

func TestWrapNilSafe(t *testing.T) {
	assert.Nil(t, Wrap(nil, "ctx"))
	assert.Nil(t, Wrapf(nil, "ctx %d", 1))
}

func TestWrapPreservesChain(t *testing.T) {
	sentinel := errors.New("sentinel")
	wrapped := Wrap(sentinel, "context")
	assert.True(t, errors.Is(wrapped, sentinel))
	assert.Equal(t, "context: sentinel", wrapped.Error())
}

func TestWrapfFormat(t *testing.T) {
	err := Wrapf(errors.New("base"), "phase %d step %s", 1, "init")
	assert.Equal(t, "phase 1 step init: base", err.Error())
}

func TestStdlibCompatibility(t *testing.T) {
	sentinel := errors.New("kafka: brokers required")
	domainErr := Config("kafka", "validate", sentinel)
	wrapped := fmt.Errorf("load config: %w", domainErr)

	assert.True(t, errors.Is(wrapped, sentinel))

	e, ok := errors.AsType[*Error](wrapped)
	require.True(t, ok)
	assert.Equal(t, KindConfig, e.Kind)
}
