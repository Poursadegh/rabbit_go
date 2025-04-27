package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strconv"
	"time"
)

type RabbitMQConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	VHost    string

	// Reconnection settings
	MaxRetries      int
	RetryInterval   time.Duration
	InitialInterval time.Duration

	// TLS settings
	UseTLS             bool
	TLSCertFile        string
	TLSKeyFile         string
	CACertFile         string
	InsecureSkipVerify bool
}

func LoadConfig() *RabbitMQConfig {
	return &RabbitMQConfig{
		Host:     getEnv("RABBITMQ_HOST", "localhost"),
		Port:     getEnv("RABBITMQ_PORT", "5672"),
		Username: getEnv("RABBITMQ_USERNAME", "guest"),
		Password: getEnv("RABBITMQ_PASSWORD", "guest"),
		VHost:    getEnv("RABBITMQ_VHOST", "/"),

		MaxRetries:      getEnvAsInt("RABBITMQ_MAX_RETRIES", 5),
		RetryInterval:   getEnvAsDuration("RABBITMQ_RETRY_INTERVAL", 5*time.Second),
		InitialInterval: getEnvAsDuration("RABBITMQ_INITIAL_INTERVAL", 1*time.Second),

		UseTLS:             getEnvAsBool("RABBITMQ_USE_TLS", false),
		TLSCertFile:        getEnv("RABBITMQ_TLS_CERT_FILE", ""),
		TLSKeyFile:         getEnv("RABBITMQ_TLS_KEY_FILE", ""),
		CACertFile:         getEnv("RABBITMQ_CA_CERT_FILE", ""),
		InsecureSkipVerify: getEnvAsBool("RABBITMQ_TLS_INSECURE_SKIP_VERIFY", false),
	}
}

func (c *RabbitMQConfig) CreateTLSConfig() (*tls.Config, error) {
	if !c.UseTLS {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: c.InsecureSkipVerify,
	}

	// Load client certificate if provided
	if c.TLSCertFile != "" && c.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.TLSCertFile, c.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Load CA certificate if provided
	if c.CACertFile != "" {
		caCert, err := os.ReadFile(c.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to append CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}
