# RabbitMQ Go Client

A professional RabbitMQ client implementation in Go that provides a clean and easy-to-use interface for working with RabbitMQ.

## Table of Contents
- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Project Structure](#project-structure)
- [Examples](#examples)
- [Contributing](#contributing)
- [License](#license)

## Features

- Connection management with automatic reconnection
- Configuration through environment variables
- Support for publishing and consuming messages
- Queue and exchange declaration
- Queue binding
- Graceful shutdown handling
- Error handling and logging
- Support for different exchange types (direct, fanout, topic, headers)

## Prerequisites

- Go 1.21 or later
- RabbitMQ server running (default configuration will connect to localhost)

## Installation

1. Clone the repository:
```bash
git clone https://github.com/yourusername/rabbitmq-go.git
cd rabbitmq-go
```

2. Install dependencies:
```bash
go mod download
```

## Configuration

The client can be configured using environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `RABBITMQ_HOST` | RabbitMQ server host | localhost |
| `RABBITMQ_PORT` | RabbitMQ server port | 5672 |
| `RABBITMQ_USERNAME` | RabbitMQ username | guest |
| `RABBITMQ_PASSWORD` | RabbitMQ password | guest |
| `RABBITMQ_VHOST` | RabbitMQ virtual host | / |

## Usage

### Basic Example

```go
package main

import (
    "log"
    "time"
    
    "github.com/yourusername/rabbitmq-go/rabbitmq"
)

func main() {
    client, err := rabbitmq.NewClient()
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Your RabbitMQ operations here
}
```

### Running Examples

```bash
go run examples/main.go
```

The example will:
1. Connect to RabbitMQ
2. Create an exchange and queue
3. Bind the queue to the exchange
4. Start a consumer that prints received messages
5. Start a publisher that sends a message every 2 seconds

## Project Structure

```
rabbitmq-go/
├── config/           # Configuration management
│   └── config.go
├── rabbitmq/         # RabbitMQ client implementation
│   ├── client.go
│   ├── consumer.go
│   └── publisher.go
├── examples/         # Example usage
│   ├── main.go
│   ├── consumer.go
│   └── publisher.go
├── go.mod
├── go.sum
└── README.md
```

## Examples

### Publishing Messages

```go
err := client.Publish("exchange_name", "routing_key", []byte("Hello, RabbitMQ!"))
if err != nil {
    log.Fatal(err)
}
```

### Consuming Messages

```go
msgs, err := client.Consume("queue_name")
if err != nil {
    log.Fatal(err)
}

for msg := range msgs {
    log.Printf("Received message: %s", msg.Body)
    msg.Ack(false)
}
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details. 