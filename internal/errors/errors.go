// Package errors provides domain-typed errors for the Quanta streaming engine.
//
// Use the domain constructors (Config, Source, Sink, Transform, Transport,
// Pipeline) at error origination points — where an error is first observed.
// Use Wrap/Wrapf when adding context while bubbling an error up.
//
// Callers inspect errors with Go 1.26's errors.AsType via Extract, IsConfig,
// IsSource, etc. These traverse the full error chain, so a domain error
// wrapped by Wrap/Wrapf is still discoverable.
package errors

import (
	"errors"
	"fmt"
)

type Kind uint8

const (
	KindConfig Kind = iota + 1
	KindTransport
	KindSource
	KindTransform
	KindSink
	KindPipeline
)

func (k Kind) String() string {
	switch k {
	case KindConfig:
		return "config"
	case KindTransport:
		return "transport"
	case KindSource:
		return "source"
	case KindTransform:
		return "transform"
	case KindSink:
		return "sink"
	case KindPipeline:
		return "pipeline"
	default:
		return "unknown"
	}
}

type Error struct {
	Kind      Kind
	Component string
	Op        string
	Err       error
}

func (e *Error) Error() string {
	if e.Component != "" {
		return fmt.Sprintf("%s[%s] %s: %v", e.Kind, e.Component, e.Op, e.Err)
	}
	return fmt.Sprintf("%s %s: %v", e.Kind, e.Op, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func Config(component, op string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: KindConfig, Component: component, Op: op, Err: err}
}

func Source(component, op string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: KindSource, Component: component, Op: op, Err: err}
}

func Sink(component, op string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: KindSink, Component: component, Op: op, Err: err}
}

func Transform(component, op string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: KindTransform, Component: component, Op: op, Err: err}
}

func Transport(component, op string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: KindTransport, Component: component, Op: op, Err: err}
}

func Pipeline(op string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: KindPipeline, Op: op, Err: err}
}

func Extract(err error) (*Error, bool) {
	return errors.AsType[*Error](err)
}

func IsKind(err error, k Kind) bool {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Kind == k
	}
	return false
}

func IsConfig(err error) bool    { return IsKind(err, KindConfig) }
func IsSource(err error) bool    { return IsKind(err, KindSource) }
func IsSink(err error) bool      { return IsKind(err, KindSink) }
func IsTransform(err error) bool { return IsKind(err, KindTransform) }
func IsTransport(err error) bool { return IsKind(err, KindTransport) }
func IsPipeline(err error) bool  { return IsKind(err, KindPipeline) }

func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}
