// Package kafka — sink-side enum types as slug-struct enums.
//
// Doctrine: Three Dots Labs "Safer Enums in Go"
// (https://threedots.tech/post/safer-enums-in-go/).
//
//   - Unexported `slug` field => downstream cannot forge invalid values.
//   - Zero value `T{}` is the explicit `Unknown*` sentinel used for defaulting.
//   - `FromString` validates against known slugs.
//   - `encoding.TextMarshaler` / `TextUnmarshaler` is the wire seam used by
//     yaml.v3 (sink configs are decoded by internal/config.DecodeYAML) and
//     by koanf.
//   - Errors flow through `qerr` with `errors.New` leaves; no `fmt.Errorf`.
package kafka

import (
	"errors"
	"strconv"
	"strings"

	qerr "quanta/internal/errors"
)

// ---------------------------------------------------------------------------
// Acks — producer durability level.
// ---------------------------------------------------------------------------

type Acks struct{ slug string }

var (
	UnknownAcks = Acks{}
	AcksNone    = Acks{"none"}
	AcksLocal   = Acks{"local"}
	AcksAll     = Acks{"all"}
)

func (v Acks) String() string { return v.slug }
func (v Acks) IsZero() bool   { return v.slug == "" }

func AcksFromString(s string) (Acks, error) {
	switch strings.ToLower(s) {
	case "":
		return UnknownAcks, nil
	case AcksNone.slug:
		return AcksNone, nil
	case AcksLocal.slug:
		return AcksLocal, nil
	case AcksAll.slug:
		return AcksAll, nil
	}
	return UnknownAcks, qerr.Config("kafka-sink", "parse-acks",
		errors.New("unknown acks "+strconv.Quote(s)+" (want one of: none, local, all)"))
}

func (v Acks) MarshalText() ([]byte, error) { return []byte(v.slug), nil }
func (v *Acks) UnmarshalText(text []byte) error {
	parsed, err := AcksFromString(string(text))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}

// ---------------------------------------------------------------------------
// Compression — producer compression codec.
// ---------------------------------------------------------------------------

type Compression struct{ slug string }

var (
	UnknownCompression = Compression{}
	CompressionNone    = Compression{"none"}
	CompressionGZIP    = Compression{"gzip"}
	CompressionSnappy  = Compression{"snappy"}
	CompressionLZ4     = Compression{"lz4"}
	CompressionZSTD    = Compression{"zstd"}
)

func (v Compression) String() string { return v.slug }
func (v Compression) IsZero() bool   { return v.slug == "" }

func CompressionFromString(s string) (Compression, error) {
	switch strings.ToLower(s) {
	case "":
		return UnknownCompression, nil
	case CompressionNone.slug:
		return CompressionNone, nil
	case CompressionGZIP.slug:
		return CompressionGZIP, nil
	case CompressionSnappy.slug:
		return CompressionSnappy, nil
	case CompressionLZ4.slug:
		return CompressionLZ4, nil
	case CompressionZSTD.slug:
		return CompressionZSTD, nil
	}
	return UnknownCompression, qerr.Config("kafka-sink", "parse-compression",
		errors.New("unknown compression "+strconv.Quote(s)+" (want one of: none, gzip, snappy, lz4, zstd)"))
}

func (v Compression) MarshalText() ([]byte, error) { return []byte(v.slug), nil }
func (v *Compression) UnmarshalText(text []byte) error {
	parsed, err := CompressionFromString(string(text))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
