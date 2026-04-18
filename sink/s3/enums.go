package s3

import (
	"errors"
	"strconv"
	"strings"

	qerr "quanta/internal/errors"
)

type AuthStrategy struct{ slug string }

var (
	UnknownAuthStrategy = AuthStrategy{}
	AuthIAMRole         = AuthStrategy{"iam-role"}
	AuthStaticCreds     = AuthStrategy{"static"}
	AuthEnvVars         = AuthStrategy{"env"}
)

func (v AuthStrategy) String() string { return v.slug }
func (v AuthStrategy) IsZero() bool   { return v.slug == "" }

func AuthStrategyFromString(s string) (AuthStrategy, error) {
	switch strings.ToLower(s) {
	case "":
		return UnknownAuthStrategy, nil
	case AuthIAMRole.slug:
		return AuthIAMRole, nil
	case AuthStaticCreds.slug:
		return AuthStaticCreds, nil
	case AuthEnvVars.slug:
		return AuthEnvVars, nil
	}
	return UnknownAuthStrategy, qerr.Config("s3", "parse-auth-strategy",
		errors.New("unknown auth_strategy "+strconv.Quote(s)+" (want one of: iam-role, static, env)"))
}

func (v AuthStrategy) MarshalText() ([]byte, error) { return []byte(v.slug), nil }
func (v *AuthStrategy) UnmarshalText(text []byte) error {
	parsed, err := AuthStrategyFromString(string(text))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}

type CompressionType struct{ slug string }

var (
	UnknownCompression = CompressionType{}
	CompressionNone    = CompressionType{"none"}
	CompressionGzip    = CompressionType{"gzip"}
	CompressionSnappy  = CompressionType{"snappy"}
)

func (v CompressionType) String() string { return v.slug }
func (v CompressionType) IsZero() bool   { return v.slug == "" }

func CompressionTypeFromString(s string) (CompressionType, error) {
	switch strings.ToLower(s) {
	case "":
		return UnknownCompression, nil
	case CompressionNone.slug:
		return CompressionNone, nil
	case CompressionGzip.slug:
		return CompressionGzip, nil
	case CompressionSnappy.slug:
		return CompressionSnappy, nil
	}
	return UnknownCompression, qerr.Config("s3", "parse-compression",
		errors.New("unknown compression "+strconv.Quote(s)+" (want one of: none, gzip, snappy)"))
}

func (v CompressionType) MarshalText() ([]byte, error) { return []byte(v.slug), nil }
func (v *CompressionType) UnmarshalText(text []byte) error {
	parsed, err := CompressionTypeFromString(string(text))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}
