package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"rabbitmq-go/config"
	"rabbitmq-go/rabbitmq"
)

func main() {
	// Load configuration
	cfg := config.LoadConfig()

	// Create AMQP client
	client, err := rabbitmq.NewAMQPClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create AMQP client: %v", err)
	}
	defer client.Close()

	// Declare exchange with TTL and dead letter exchange
	exchangeArgs := map[string]interface{}{
		"x-message-ttl":             60000, // 60 seconds
		"x-dead-letter-exchange":    "dlx",
		"x-dead-letter-routing-key": "dlq",
	}

	if err := client.ExchangeDeclare(
		"test_exchange",
		"direct",
		true,  // durable
		false, // autoDelete
		false, // internal
		false, // noWait
		exchangeArgs,
	); err != nil {
		log.Fatalf("Failed to declare exchange: %v", err)
	}

	// Declare dead letter exchange
	if err := client.ExchangeDeclare(
		"dlx",
		"direct",
		true,  // durable
		false, // autoDelete
		false, // internal
		false, // noWait
		nil,
	); err != nil {
		log.Fatalf("Failed to declare dead letter exchange: %v", err)
	}

	// Declare main queue with arguments
	queueArgs := map[string]interface{}{
		"x-message-ttl":             30000, // 30 seconds
		"x-max-length":              1000,
		"x-max-length-bytes":        1000000,
		"x-dead-letter-exchange":    "dlx",
		"x-dead-letter-routing-key": "dlq",
	}

	if err := client.QueueDeclare(
		"test_queue",
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		queueArgs,
	); err != nil {
		log.Fatalf("Failed to declare queue: %v", err)
	}

	// Declare dead letter queue
	if err := client.QueueDeclare(
		"dlq",
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,
	); err != nil {
		log.Fatalf("Failed to declare dead letter queue: %v", err)
	}

	// Bind queues to exchanges
	if err := client.QueueBind(
		"test_queue",
		"test_exchange",
		"test_routing_key",
		false, // noWait
		nil,
	); err != nil {
		log.Fatalf("Failed to bind queue: %v", err)
	}

	if err := client.QueueBind(
		"dlq",
		"dlx",
		"dlq",
		false, // noWait
		nil,
	); err != nil {
		log.Fatalf("Failed to bind dead letter queue: %v", err)
	}

	// Start consumer for main queue
	go func() {
		if err := client.Consume("test_queue", func(body []byte) error {
			fmt.Printf("Received message from main queue: %s\n", string(body))
			return nil
		}); err != nil {
			log.Fatalf("Failed to start consumer: %v", err)
		}
	}()

	// Start consumer for dead letter queue
	go func() {
		if err := client.Consume("dlq", func(body []byte) error {
			fmt.Printf("Received message from dead letter queue: %s\n", string(body))
			return nil
		}); err != nil {
			log.Fatalf("Failed to start dead letter consumer: %v", err)
		}
	}()

	// Start publisher
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			message := fmt.Sprintf("Hello at %s", time.Now().Format(time.RFC3339))
			if err := client.Publish("test_exchange", "test_routing_key", []byte(message)); err != nil {
				log.Printf("Failed to publish message: %v", err)
			}
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("Shutting down...")
}
