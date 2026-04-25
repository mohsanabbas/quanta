package errors

import (
	stderrors "errors"
	"testing"
)

var sinkErr error

func BenchmarkOriginConstructor(b *testing.B) {
	cause := stderrors.New("connection refused")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkErr = Source("kafka", "dial", cause)
	}
}

func BenchmarkWrap(b *testing.B) {
	base := Source("kafka", "dial", stderrors.New("connection refused"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkErr = Wrap(base, "pipeline compile")
	}
}

func BenchmarkPublic(b *testing.B) {
	e, _ := Extract(Source("kafka", "dial", stderrors.New("x")))
	b.ReportAllocs()
	b.ResetTimer()
	var s string
	for i := 0; i < b.N; i++ {
		s = e.Public()
	}
	_ = s
}

func BenchmarkExtract(b *testing.B) {
	err := Wrap(Wrap(Source("kafka", "dial", stderrors.New("x")), "phase1"), "phase2")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Extract(err)
	}
}

func BenchmarkOpaque(b *testing.B) {
	cause := stderrors.New("internal: broker timeout")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkErr = Opaque(KindTransport, "transport unavailable", cause)
	}
}
