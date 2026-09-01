// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package loadbalancingexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/loadbalancingexporter"

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/servicediscovery/types"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/config/configspa"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/exporter/otlpexporter"
)

type routingKey int

const (
	traceIDRouting routingKey = iota
	svcRouting
	metricNameRouting
	resourceRouting
	streamIDRouting
	attrRouting
)

const (
	svcRoutingStr        = "service"
	traceIDRoutingStr    = "traceID"
	metricNameRoutingStr = "metric"
	resourceRoutingStr   = "resource"
	streamIDRoutingStr   = "streamID"
	attrRoutingStr       = "attributes"
)

// Config defines configuration for the exporter.
type Config struct {
	TimeoutSettings           exporterhelper.TimeoutConfig `mapstructure:",squash"`
	configretry.BackOffConfig `mapstructure:"retry_on_failure"`
	QueueSettings             configoptional.Optional[exporterhelper.QueueBatchConfig] `mapstructure:"sending_queue"`

	Protocol Protocol         `mapstructure:"protocol"`
	Resolver ResolverSettings `mapstructure:"resolver"`

	// RoutingKey is a single routing key value
	RoutingKey string `mapstructure:"routing_key"`

	// RoutingAttributes creates a composite routing key from the listed attributes.
	//
	// For traces, attributes can come from resource, scope, or span, plus the pseudo attributes "span.kind" and
	// "span.name".
	// For logs, attributes can come from resource, scope, or log record attributes, plus the pseudo attributes
	// "log.severity" and "log.body".
	// For metrics, attributes can come from resource, scope, or datapoint attributes.
	// Keys are encoded as "name=value|name=value|" in the order configured. Missing attributes are encoded as "name=|".
	// Non-string values are deterministically stringified.
	RoutingAttributes []string `mapstructure:"routing_attributes"`

	// SPAPerEndpoint maps a resolved backend endpoint to a per-endpoint SPA config.
	// When set for an endpoint, it overrides Protocol.OTLP.ClientConfig.SPA for the
	// sub-exporter created for that backend. Keys are matched after the same
	// host:port normalization the load balancer applies to resolver output.
	// Intended for cases (typically the static resolver) where each backend has its
	// own PSK; DNS/k8s/aws deployments usually share one PSK via protocol.otlp.spa.
	SPAPerEndpoint map[string]*configspa.Config `mapstructure:"spa_per_endpoint,omitempty"`
}

// Validate checks if the exporter configuration is valid.
func (c *Config) Validate() error {
	// routing_attributes only has meaning when routing_key=attributes.
	if c.RoutingKey == attrRoutingStr && len(c.RoutingAttributes) == 0 {
		return fmt.Errorf("routing_attributes must be specified when routing_key is %q", attrRoutingStr)
	}

	if c.RoutingKey != attrRoutingStr && len(c.RoutingAttributes) > 0 {
		return fmt.Errorf("routing_attributes can only be used when routing_key is %q; got %q. Remove routing_attributes or set routing_key to %q", attrRoutingStr, c.RoutingKey, attrRoutingStr)
	}

	// Normalize spa_per_endpoint keys with the same host:port rule the load
	// balancer applies to resolver output, and validate each SPA entry.
	if len(c.SPAPerEndpoint) > 0 {
		normalized := make(map[string]*configspa.Config, len(c.SPAPerEndpoint))
		for endpoint, spa := range c.SPAPerEndpoint {
			if spa == nil {
				return fmt.Errorf("spa_per_endpoint: entry for %q must not be null", endpoint)
			}
			if err := spa.Validate(); err != nil {
				return fmt.Errorf("spa_per_endpoint[%q]: %w", endpoint, err)
			}
			key := endpointWithPort(endpoint)
			if _, dup := normalized[key]; dup {
				return fmt.Errorf("spa_per_endpoint: duplicate entry for endpoint %q after host:port normalization", key)
			}
			normalized[key] = spa
		}
		c.SPAPerEndpoint = normalized
	}

	return nil
}

// Protocol holds the individual protocol-specific settings. Only OTLP is supported at the moment.
type Protocol struct {
	OTLP otlpexporter.Config `mapstructure:"otlp"`
	// prevent unkeyed literal initialization
	_ struct{}
}

// ResolverSettings defines the configurations for the backend resolver
type ResolverSettings struct {
	Static      configoptional.Optional[StaticResolver]      `mapstructure:"static"`
	DNS         configoptional.Optional[DNSResolver]         `mapstructure:"dns"`
	K8sSvc      configoptional.Optional[K8sSvcResolver]      `mapstructure:"k8s"`
	AWSCloudMap configoptional.Optional[AWSCloudMapResolver] `mapstructure:"aws_cloud_map"`
	// prevent unkeyed literal initialization
	_ struct{}
}

// StaticResolver defines the configuration for the resolver providing a fixed list of backends
type StaticResolver struct {
	Hostnames []string `mapstructure:"hostnames"`
	// prevent unkeyed literal initialization
	_ struct{}
}

// DNSResolver defines the configuration for the DNS resolver
type DNSResolver struct {
	Hostname string        `mapstructure:"hostname"`
	Port     string        `mapstructure:"port"`
	Interval time.Duration `mapstructure:"interval"`
	Timeout  time.Duration `mapstructure:"timeout"`
	// prevent unkeyed literal initialization
	_ struct{}
}

// K8sSvcResolver defines the configuration for the DNS resolver
type K8sSvcResolver struct {
	Service         string        `mapstructure:"service"`
	Ports           []int32       `mapstructure:"ports"`
	Timeout         time.Duration `mapstructure:"timeout"`
	ReturnHostnames bool          `mapstructure:"return_hostnames"`
	// prevent unkeyed literal initialization
	_ struct{}
}

type AWSCloudMapResolver struct {
	NamespaceName string                   `mapstructure:"namespace"`
	ServiceName   string                   `mapstructure:"service_name"`
	HealthStatus  types.HealthStatusFilter `mapstructure:"health_status"`
	Interval      time.Duration            `mapstructure:"interval"`
	Timeout       time.Duration            `mapstructure:"timeout"`
	Port          *uint16                  `mapstructure:"port"`
	OwnerAccount  *string                  `mapstructure:"owner_account"`
}
