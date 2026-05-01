package clickhouse

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

func (c *Config) buildTLSConfig() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: c.TLSInsecure, //nolint:gosec // User-controlled for dev environments
	}

	// Load CA certificate
	if c.CACert != "" {
		caCert, err := os.ReadFile(c.CACert)
		if err != nil {
			return nil, fmt.Errorf("read ca_cert: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("invalid ca_cert: failed to parse PEM")
		}
		tlsConfig.RootCAs = caCertPool
	}

	// Load client certificate (mTLS)
	if c.ClientCert != "" && c.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(c.ClientCert, c.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}
