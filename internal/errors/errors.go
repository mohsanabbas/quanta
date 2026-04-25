// Package errors provides domain-typed errors for the Quanta streaming engine.
//
// # Two-message model
//
// Every *Error carries two messages: a developer message (Error) that
// includes kind, component, op and the wrapped cause, and a sanitized
// public message (Public) safe to expose at trust boundaries. Internal
// logs and in-process propagation use Error; anything crossing a
// service or API boundary uses Public, so internal topology (broker
// hostnames, query strings, file paths) never reaches end users.
//
// # Stack traces
//
// Origin constructors capture a call stack at construction time, up
// to a fixed depth. The stack is exposed via the Stack method on
// *Error and on opaqueError, and the top frame is included as an
// "origin" attribute by the slog.LogValuer implementations. The %+v
// verb on *Error prints the stack inline, matching the convention
// popularised by pkg/errors.
//
// Wrap and Wrapf do not capture stacks of their own: the origin
// stack on the underlying *Error is the canonical one, and
// re-capturing at every wrap layer is noise. If you need a fresh
// stack at a wrap point, use Opaque or one of the origin
// constructors instead.
//
// # Opaque wrapping
//
// Opaque produces a wrapper that deliberately omits Unwrap. errors.Is,
// errors.As and errors.AsType cannot reach the hidden cause from
// outside this package. Use Opaque at trust boundaries — when an
// internal error is about to cross into a less-trusted caller, and
// you want to ensure the caller cannot probe internal detail by
// matching error types. The hidden cause is retained for structured
// logging via LogValue and the unexported Cause accessor.
//
// # Constructors
//
// Use the domain constructors (Config, Source, Sink, Transform,
// Transport, Pipeline) at error origination — where an error is first
// observed. Use Wrap or Wrapf to add free-form context as the error
// bubbles up; both preserve the chain so the original *Error is still
// discoverable downstream via Extract.
//
// # Inspection
//
// Callers inspect errors with errors.AsType, surfaced via
// Extract for the typed payload (Component, Op, Kind, …). Kind
// matching uses IsKind or one of the IsConfig / IsTransport / …
// helpers, all of which traverse the full chain via errors.AsType.
//
// # Structured logging
//
// *Error implements slog.LogValuer, so passing an *Error to a
// structured logger emits kind / component / op / cause as separate
// fields rather than a flattened string. This makes errors trivially
// queryable in JSON log pipelines.
package errors

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
)

type Kind struct {
	name   string
	public string
}

func (k Kind) String() string {
	if k.name == "" {
		return unknown
	}
	return k.name
}

func (k Kind) IsZero() bool {
	return k.name == ""
}

var (
	KindUnknown = Kind{}
	KindConfig  = Kind{
		name:   "config",
		public: "configuration error",
	}
	KindTransport = Kind{
		name:   "transport",
		public: "transport error",
	}
	KindSource = Kind{
		name:   "source",
		public: "ingress error",
	}
	KindTransform = Kind{
		name:   "transform",
		public: "processing error",
	}
	KindSink = Kind{
		name:   "sink",
		public: "egress error",
	}
	KindPipeline = Kind{
		name:   "pipeline",
		public: "pipeline error",
	}
)

func KindFromString(s string) (Kind, error) {
	switch s {
	case KindConfig.name:
		return KindConfig, nil
	case KindTransport.name:
		return KindTransport, nil
	case KindSource.name:
		return KindSource, nil
	case KindTransform.name:
		return KindTransform, nil
	case KindSink.name:
		return KindSink, nil
	case KindPipeline.name:
		return KindPipeline, nil
	}
	return KindUnknown, fmt.Errorf("unknown kind: %q", s)
}

type Error struct {
	Component string

	Op string

	Err error

	Kind Kind

	pcs [stackDepth]uintptr
	npc int
}

var (
	_ error          = (*Error)(nil)
	_ slog.LogValuer = (*Error)(nil)
	_ fmt.Formatter  = (*Error)(nil)
)

const stackDepth = 16

const (
	modulePath = "quanta"
	unknown    = "unknown"
)

const (
	newErrSkip = 3
	opaqueSkip = 2
)

func (e *Error) pcSlice() []uintptr {
	if e == nil || e.npc == 0 {
		return nil
	}
	return e.pcs[:e.npc]
}

func resolveStack(pcs []uintptr) []runtime.Frame {
	if len(pcs) == 0 {
		return nil
	}
	frames := runtime.CallersFrames(pcs)
	out := make([]runtime.Frame, 0, len(pcs))
	for {
		f, more := frames.Next()
		out = append(out, f)
		if !more {
			break
		}
	}
	return out
}

func originFrame(pcs []uintptr) string {
	if len(pcs) == 0 {
		return ""
	}
	frames := runtime.CallersFrames(pcs)
	f, _ := frames.Next()
	if f.Function == "" && f.File == "" {
		return ""
	}
	return frameOrigin(f)
}

func frameOrigin(f runtime.Frame) string {
	return fmt.Sprintf("%s:%d %s", sourceFile(f.File), f.Line, f.Function)
}

func sourceFile(file string) string {
	if file == "" {
		return unknown
	}
	file = strings.ReplaceAll(file, "\\", "/")
	if rest, ok := strings.CutPrefix(file, modulePath+"/"); ok {
		return rest
	}
	if index := strings.LastIndex(file, "/"+modulePath+"/"); index >= 0 {
		return file[index+len(modulePath)+2:]
	}
	if index := strings.LastIndex(file, "/src/"); index >= 0 {
		return file[index+len("/src/"):]
	}
	if index := strings.LastIndexByte(file, '/'); index >= 0 && index+1 < len(file) {
		return file[index+1:]
	}
	return file
}

func (e *Error) Stack() []runtime.Frame {
	return resolveStack(e.pcSlice())
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	switch {
	case e.Err == nil && e.Component == "" && e.Op == "":
		return e.Kind.String()
	case e.Err == nil && e.Component == "":
		return fmt.Sprintf("%s %s", e.Kind, e.Op)
	case e.Err == nil:
		return fmt.Sprintf("%s[%s] %s", e.Kind, e.Component, e.Op)
	case e.Component == "":
		return fmt.Sprintf("%s %s: %v", e.Kind, e.Op, e.Err)
	default:
		return fmt.Sprintf("%s[%s] %s: %v", e.Kind, e.Component, e.Op, e.Err)
	}
}

func (e *Error) Format(s fmt.State, verb rune) {
	if e == nil {
		_, _ = fmt.Fprint(s, "<nil>")
		return
	}
	switch verb {
	case 'v':
		if s.Flag('+') {
			_, _ = fmt.Fprint(s, e.Error())
			for _, f := range e.Stack() {
				_, _ = fmt.Fprintf(s, "\n\t%s\n\t\t%s:%d", f.Function, sourceFile(f.File), f.Line)
			}
			return
		}
		fallthrough
	case 's':
		_, _ = fmt.Fprint(s, e.Error())
	case 'q':
		_, _ = fmt.Fprintf(s, "%q", e.Error())
	}
}

func (e *Error) Public() string {
	if e == nil || e.Kind.public == "" {
		return "internal error"
	}
	return e.Kind.public
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *Error) LogValue() slog.Value {
	if e == nil {
		return slog.StringValue("<nil>")
	}
	attrs := make([]slog.Attr, 0, 5)
	attrs = append(attrs, slog.String("kind", e.Kind.String()))
	if e.Component != "" {
		attrs = append(attrs, slog.String("component", e.Component))
	}
	if e.Op != "" {
		attrs = append(attrs, slog.String("op", e.Op))
	}
	if e.Err != nil {
		attrs = append(attrs, slog.String("cause", e.Err.Error()))
	}
	if origin := originFrame(e.pcSlice()); origin != "" {
		attrs = append(attrs, slog.String("origin", origin))
	}
	return slog.GroupValue(attrs...)
}

func newErr(kind Kind, component, op string, err error) error {
	if err == nil {
		return nil
	}
	e := &Error{
		Kind:      kind,
		Component: component,
		Op:        op,
		Err:       err,
	}
	e.npc = runtime.Callers(newErrSkip, e.pcs[:])
	return e
}

func Config(component, op string, err error) error {
	return newErr(KindConfig, component, op, err)
}

func Source(component, op string, err error) error {
	return newErr(KindSource, component, op, err)
}

func Sink(component, op string, err error) error {
	return newErr(KindSink, component, op, err)
}

func Transform(component, op string, err error) error {
	return newErr(KindTransform, component, op, err)
}

func Transport(component, op string, err error) error {
	return newErr(KindTransport, component, op, err)
}

func Pipeline(op string, err error) error {
	return newErr(KindPipeline, "", op, err)
}

func Extract(err error) (*Error, bool) {
	return errors.AsType[*Error](err)
}

func IsKind(err error, k Kind) bool {
	e, ok := errors.AsType[*Error](err)
	return ok && e.Kind == k
}

func IsConfig(err error) bool {
	return IsKind(err, KindConfig)
}

func IsSource(err error) bool {
	return IsKind(err, KindSource)
}

func IsSink(err error) bool {
	return IsKind(err, KindSink)
}

func IsTransform(err error) bool {
	return IsKind(err, KindTransform)
}

func IsTransport(err error) bool {
	return IsKind(err, KindTransport)
}

func IsPipeline(err error) bool {
	return IsKind(err, KindPipeline)
}

type opaqueError struct {
	kind Kind

	publicMsg string

	hidden error

	pcs [stackDepth]uintptr
	npc int
}

func (e *opaqueError) pcSlice() []uintptr {
	if e == nil || e.npc == 0 {
		return nil
	}
	return e.pcs[:e.npc]
}

var (
	_ error          = (*opaqueError)(nil)
	_ slog.LogValuer = (*opaqueError)(nil)
)

func (e *opaqueError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.publicMsg
}

func (e *opaqueError) Public() string {
	if e == nil {
		return "internal error"
	}
	return e.publicMsg
}

func (e *opaqueError) Cause() error {
	if e == nil {
		return nil
	}
	return e.hidden
}

func (e *opaqueError) Stack() []runtime.Frame {
	return resolveStack(e.pcSlice())
}

func (e *opaqueError) LogValue() slog.Value {
	if e == nil {
		return slog.StringValue("<nil>")
	}
	attrs := make([]slog.Attr, 0, 4)
	attrs = append(attrs, slog.String("kind", e.kind.String()))
	attrs = append(attrs, slog.String("public", e.publicMsg))
	if e.hidden != nil {
		attrs = append(attrs, slog.String("hidden_cause", e.hidden.Error()))
	}
	if origin := originFrame(e.pcSlice()); origin != "" {
		attrs = append(attrs, slog.String("origin", origin))
	}
	return slog.GroupValue(attrs...)
}

// Opaque captures its stack inline; skip=2 (Callers, Opaque, caller).
func Opaque(kind Kind, publicMsg string, cause error) error {
	if cause == nil {
		return nil
	}
	e := &opaqueError{
		kind:      kind,
		publicMsg: publicMsg,
		hidden:    cause,
	}
	e.npc = runtime.Callers(opaqueSkip, e.pcs[:])
	return e
}

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
