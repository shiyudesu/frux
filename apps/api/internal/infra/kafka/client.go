package infrakafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

var (
	ErrKafkaDisabled    = errors.New("kafka is disabled")
	ErrKafkaUnavailable = errors.New("kafka is unavailable")
	ErrKafkaShutdown    = errors.New("kafka shutdown failed")
	ErrInvalidKafkaTLS  = errors.New("invalid kafka TLS configuration")
)

type Client struct {
	kgoClient       *kgo.Client
	adminTimeout    time.Duration
	produceTimeout  time.Duration
	shutdownTimeout time.Duration
	topicPrefix     string
	environment     string
	closeOnce       sync.Once
	closeErr        error
}

func NewClient(ctx context.Context, cfg infraconfig.KafkaConfig) (*Client, error) {
	if !cfg.Enabled {
		return nil, ErrKafkaDisabled
	}
	options, err := clientOptions(cfg)
	if err != nil {
		return nil, err
	}
	client, err := kgo.NewClient(options...)
	if err != nil {
		return nil, fmt.Errorf("%w: initialize client", ErrKafkaUnavailable)
	}
	dialTimeout, err := time.ParseDuration(cfg.Timeouts.Dial)
	if err != nil {
		client.Close()
		return nil, err
	}
	pingContext, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	if err := client.Ping(pingContext); err != nil {
		client.Close()
		return nil, fmt.Errorf("%w: broker ping", ErrKafkaUnavailable)
	}
	adminTimeout, err := time.ParseDuration(cfg.Timeouts.Admin)
	if err != nil {
		client.Close()
		return nil, err
	}
	produceTimeout, err := time.ParseDuration(cfg.Timeouts.Produce)
	if err != nil {
		client.Close()
		return nil, err
	}
	shutdownTimeout, err := time.ParseDuration(cfg.Timeouts.Shutdown)
	if err != nil {
		client.Close()
		return nil, err
	}
	return &Client{
		kgoClient: client, adminTimeout: adminTimeout,
		produceTimeout: produceTimeout, shutdownTimeout: shutdownTimeout,
		topicPrefix: cfg.TopicPrefix, environment: cfg.Environment,
	}, nil
}

func clientOptions(cfg infraconfig.KafkaConfig) ([]kgo.Opt, error) {
	dialTimeout, err := time.ParseDuration(cfg.Timeouts.Dial)
	if err != nil {
		return nil, err
	}
	requestTimeout, err := time.ParseDuration(cfg.Timeouts.Request)
	if err != nil {
		return nil, err
	}
	produceTimeout, err := time.ParseDuration(cfg.Timeouts.Produce)
	if err != nil {
		return nil, err
	}
	options := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		kgo.DialTimeout(dialTimeout),
		kgo.RequestTimeoutOverhead(requestTimeout),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.RecordRetries(5),
		kgo.RecordDeliveryTimeout(produceTimeout),
		kgo.ProduceRequestTimeout(produceTimeout),
		kgo.AllowIdempotentProduceCancellation(),
		kgo.ProducerBatchMaxBytes(producerBatchMaxBytes()),
	}

	switch cfg.Authentication.Mechanism {
	case "", "none":
	case "plain":
		options = append(options, kgo.SASL(plain.Auth{
			User: cfg.Authentication.Username, Pass: cfg.Authentication.Password,
		}.AsMechanism()))
	case "scram-sha-256":
		options = append(options, kgo.SASL(scram.Auth{
			User: cfg.Authentication.Username, Pass: cfg.Authentication.Password,
		}.AsSha256Mechanism()))
	case "scram-sha-512":
		options = append(options, kgo.SASL(scram.Auth{
			User: cfg.Authentication.Username, Pass: cfg.Authentication.Password,
		}.AsSha512Mechanism()))
	default:
		return nil, fmt.Errorf("%w: authentication mechanism", ErrKafkaUnavailable)
	}
	if cfg.TLS.Enabled {
		tlsConfig, err := loadTLSConfig(cfg.TLS)
		if err != nil {
			return nil, err
		}
		options = append(options, kgo.DialTLSConfig(tlsConfig))
	}
	return options, nil
}

func producerBatchMaxBytes() int32 {
	var minimum int
	for _, topic := range Topics() {
		limit := brokerMaxMessageBytes(topic)
		if minimum == 0 || limit < minimum {
			minimum = limit
		}
	}
	if minimum <= 0 {
		minimum = 1 << 20
	}
	return int32(minimum)
}

func loadTLSConfig(cfg infraconfig.KafkaTLSConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         cfg.ServerName,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("%w: read CA", ErrInvalidKafkaTLS)
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%w: parse CA", ErrInvalidKafkaTLS)
		}
		tlsConfig.RootCAs = pool
	}
	if cfg.CertificateFile != "" {
		certificate, err := tls.LoadX509KeyPair(cfg.CertificateFile, cfg.PrivateKeyFile)
		if err != nil {
			return nil, fmt.Errorf("%w: client certificate", ErrInvalidKafkaTLS)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	return tlsConfig, nil
}

func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.kgoClient == nil {
		return ErrKafkaUnavailable
	}
	if err := c.kgoClient.Ping(ctx); err != nil {
		return fmt.Errorf("%w: broker ping", ErrKafkaUnavailable)
	}
	return nil
}

func (c *Client) Close(ctx context.Context) error {
	if c == nil || c.kgoClient == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		timeout := c.shutdownTimeout
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		closeContext, cancel := context.WithTimeout(ctx, timeout)
		flushErr := c.kgoClient.Flush(closeContext)
		cancel()
		c.kgoClient.Close()
		if flushErr != nil {
			c.closeErr = fmt.Errorf("%w: %v", ErrKafkaShutdown, sanitizeKafkaError(flushErr))
		}
	})
	return c.closeErr
}

func sanitizeKafkaError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case strings.Contains(message, "auth"):
		return "authentication_failed"
	case strings.Contains(message, "tls") || strings.Contains(message, "certificate"):
		return "tls_failed"
	case strings.Contains(message, "unknown topic"):
		return "unknown_topic"
	case strings.Contains(message, "not leader"):
		return "leader_unavailable"
	default:
		return "broker_error"
	}
}
