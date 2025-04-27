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

	// Start consumer
	go func() {
		if err := client.Consume("test_queue", func(body []byte) error {
			fmt.Printf("Received message: %s\n", string(body))
			return nil
		}); err != nil {
			log.Fatalf("Failed to start consumer: %v", err)
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
