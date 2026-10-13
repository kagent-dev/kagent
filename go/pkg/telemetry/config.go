package telemetry

import (
	"fmt"
	"net/url"
	"time"
)

// Config selects each exporter explicitly. An absent signal is disabled.
// Runtime JSON uses this Go contract; controller protobufs are converted at startup.
type Config struct {
	Traces             *OTLPConfig       `json:"traces,omitempty"`
	Metrics            *OTLPConfig       `json:"metrics,omitempty"`
	Logs               *OTLPConfig       `json:"logs,omitempty"`
	ResourceAttributes map[string]string `json:"resource_attributes,omitempty"`
}

type OTLPConfig struct {
	Endpoint      string `json:"endpoint"`
	Protocol      string `json:"protocol"`
	TimeoutMillis int64  `json:"timeout_ms,omitempty"`
}

func (c Config) Validate() error {
	for name, signal := range map[string]*OTLPConfig{"traces": c.Traces, "metrics": c.Metrics, "logs": c.Logs} {
		if signal == nil {
			continue
		}
		u, err := url.Parse(signal.Endpoint)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("telemetry.%s.endpoint must be an HTTP(S) URL", name)
		}
		if signal.Protocol != "grpc" && signal.Protocol != "http/protobuf" {
			return fmt.Errorf("telemetry.%s.protocol must be grpc or http/protobuf", name)
		}
		if signal.TimeoutMillis < 0 || signal.TimeoutMillis > int64((24*time.Hour)/time.Millisecond) {
			return fmt.Errorf("telemetry.%s.timeout_ms must be between 0 and 86400000", name)
		}
	}
	return nil
}

func (c OTLPConfig) timeout() time.Duration {
	if c.TimeoutMillis == 0 {
		return 10 * time.Second
	}
	return time.Duration(c.TimeoutMillis) * time.Millisecond
}
