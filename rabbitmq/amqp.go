package rabbitmq

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"rabbitmq-go/config"
	"sync"
	"time"
)

const (
	AMQP_PROTOCOL_HEADER = "AMQP\x00\x00\x09\x01"
	FRAME_METHOD         = 1
	FRAME_HEADER         = 2
	FRAME_BODY           = 3
	FRAME_HEARTBEAT      = 8
	FRAME_END            = 0xCE
)

type Frame struct {
	Type    byte
	Channel uint16
	Size    uint32
	Payload []byte
	End     byte
}

type AMQPClient struct {
	conn         net.Conn
	reader       *bufio.Reader
	writer       *bufio.Writer
	channel      uint16
	config       *config.RabbitMQConfig
	mu           sync.RWMutex
	done         chan struct{}
	reconnecting bool
}

func NewAMQPClient(cfg *config.RabbitMQConfig) (*AMQPClient, error) {
	client := &AMQPClient{
		config: cfg,
		done:   make(chan struct{}),
	}

	if err := client.connect(); err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	// Start reconnection monitor
	go client.monitorConnection()

	return client, nil
}

func (c *AMQPClient) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
	}

	var conn net.Conn
	var err error

	if c.config.UseTLS {
		tlsConfig, err := c.config.CreateTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to create TLS config: %w", err)
		}

		// Use TLS port if not specified
		port := c.config.Port
		if port == "5672" {
			port = "5671"
		}

		conn, err = tls.Dial("tcp", fmt.Sprintf("%s:%s", c.config.Host, port), tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to establish TLS connection: %w", err)
		}

		// Verify TLS connection
		if err := conn.(*tls.Conn).Handshake(); err != nil {
			conn.Close()
			return fmt.Errorf("TLS handshake failed: %w", err)
		}
	} else {
		conn, err = net.Dial("tcp", fmt.Sprintf("%s:%s", c.config.Host, c.config.Port))
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
	}

	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.writer = bufio.NewWriter(conn)

	if err := c.handshake(); err != nil {
		conn.Close()
		return fmt.Errorf("handshake failed: %w", err)
	}

	return nil
}

func (c *AMQPClient) reconnect() error {
	c.mu.Lock()
	c.reconnecting = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.reconnecting = false
		c.mu.Unlock()
	}()

	interval := c.config.InitialInterval
	for i := 0; i < c.config.MaxRetries; i++ {
		log.Printf("Attempting to reconnect (attempt %d/%d)...", i+1, c.config.MaxRetries)

		if err := c.connect(); err == nil {
			log.Println("Successfully reconnected to RabbitMQ")
			return nil
		}

		if i < c.config.MaxRetries-1 {
			log.Printf("Reconnection failed, retrying in %v...", interval)
			time.Sleep(interval)
			interval = c.config.RetryInterval
		}
	}

	return fmt.Errorf("failed to reconnect after %d attempts", c.config.MaxRetries)
}

func (c *AMQPClient) monitorConnection() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.RLock()
			reconnecting := c.reconnecting
			c.mu.RUnlock()

			if !reconnecting {
				if err := c.checkConnection(); err != nil {
					log.Printf("Connection check failed: %v", err)
					if err := c.reconnect(); err != nil {
						log.Printf("Failed to reconnect: %v", err)
					}
				}
			}
		}
	}
}

func (c *AMQPClient) checkConnection() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil {
		return fmt.Errorf("connection is nil")
	}

	// Try to read from the connection to check if it's still alive
	c.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, err := c.reader.Peek(1)
	if err != nil {
		return fmt.Errorf("connection check failed: %w", err)
	}
	c.conn.SetReadDeadline(time.Time{})

	return nil
}

func (c *AMQPClient) handshake() error {
	// Send protocol header
	if _, err := c.writer.Write([]byte(AMQP_PROTOCOL_HEADER)); err != nil {
		return err
	}
	if err := c.writer.Flush(); err != nil {
		return err
	}

	// Read server response
	response := make([]byte, 8)
	if _, err := c.reader.Read(response); err != nil {
		return err
	}

	if !bytes.Equal(response, []byte(AMQP_PROTOCOL_HEADER)) {
		return fmt.Errorf("invalid protocol header response")
	}

	return nil
}

func (c *AMQPClient) sendFrame(frame *Frame) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, frame.Type)
	binary.Write(buf, binary.BigEndian, frame.Channel)
	binary.Write(buf, binary.BigEndian, frame.Size)
	buf.Write(frame.Payload)
	binary.Write(buf, binary.BigEndian, frame.End)

	_, err := c.writer.Write(buf.Bytes())
	if err != nil {
		return err
	}
	return c.writer.Flush()
}

func (c *AMQPClient) readFrame() (*Frame, error) {
	header := make([]byte, 7)
	if _, err := c.reader.Read(header); err != nil {
		return nil, err
	}

	frame := &Frame{
		Type:    header[0],
		Channel: binary.BigEndian.Uint16(header[1:3]),
		Size:    binary.BigEndian.Uint32(header[3:7]),
	}

	if frame.Size > 0 {
		frame.Payload = make([]byte, frame.Size)
		if _, err := c.reader.Read(frame.Payload); err != nil {
			return nil, err
		}
	}

	endByte := make([]byte, 1)
	if _, err := c.reader.Read(endByte); err != nil {
		return nil, err
	}
	frame.End = endByte[0]

	return frame, nil
}

func (c *AMQPClient) Close() error {
	close(c.done)
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *AMQPClient) Publish(exchange, routingKey string, message []byte) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	// Basic.Publish method
	method := []byte{
		0x00, 0x3C, 0x00, 0x28, // Basic.Publish
		0x00, 0x00, // reserved-1
	}

	// Add exchange length and name
	method = append(method, byte(len(exchange)))
	method = append(method, []byte(exchange)...)

	// Add routing key length and name
	method = append(method, byte(len(routingKey)))
	method = append(method, []byte(routingKey)...)

	// Add mandatory and immediate flags
	method = append(method, 0x00, 0x00)

	frame := &Frame{
		Type:    FRAME_METHOD,
		Channel: c.channel,
		Size:    uint32(len(method)),
		Payload: method,
		End:     FRAME_END,
	}

	if err := c.sendFrame(frame); err != nil {
		return fmt.Errorf("failed to send method frame: %w", err)
	}

	// Send content header
	header := []byte{
		0x00, 0x3C, // Basic class
		0x00, 0x00, // weight
	}
	binary.BigEndian.PutUint64(header[4:], uint64(len(message)))
	header = append(header, 0x00, 0x00, 0x00, 0x00) // property flags

	frame = &Frame{
		Type:    FRAME_HEADER,
		Channel: c.channel,
		Size:    uint32(len(header)),
		Payload: header,
		End:     FRAME_END,
	}

	if err := c.sendFrame(frame); err != nil {
		return fmt.Errorf("failed to send header frame: %w", err)
	}

	// Send body
	frame = &Frame{
		Type:    FRAME_BODY,
		Channel: c.channel,
		Size:    uint32(len(message)),
		Payload: message,
		End:     FRAME_END,
	}

	if err := c.sendFrame(frame); err != nil {
		return fmt.Errorf("failed to send body frame: %w", err)
	}

	return nil
}

func (c *AMQPClient) Consume(queue string, handler func([]byte) error) error {
	c.mu.RLock()
	if c.conn == nil {
		c.mu.RUnlock()
		return fmt.Errorf("not connected to RabbitMQ")
	}
	c.mu.RUnlock()

	// Basic.Consume method
	method := []byte{
		0x00, 0x3C, 0x00, 0x14, // Basic.Consume
		0x00, 0x00, // reserved-1
	}

	// Add queue length and name
	method = append(method, byte(len(queue)))
	method = append(method, []byte(queue)...)

	// Add consumer tag, no-local, no-ack, exclusive, no-wait flags
	method = append(method, 0x00)                   // consumer tag length
	method = append(method, 0x00, 0x00, 0x01, 0x00) // flags

	frame := &Frame{
		Type:    FRAME_METHOD,
		Channel: c.channel,
		Size:    uint32(len(method)),
		Payload: method,
		End:     FRAME_END,
	}

	if err := c.sendFrame(frame); err != nil {
		return fmt.Errorf("failed to send consume method: %w", err)
	}

	// Start consuming messages in a separate goroutine
	go func() {
		for {
			select {
			case <-c.done:
				return
			default:
				c.mu.RLock()
				if c.conn == nil {
					c.mu.RUnlock()
					time.Sleep(1 * time.Second)
					continue
				}

				frame, err := c.readFrame()
				c.mu.RUnlock()

				if err != nil {
					log.Printf("Error reading frame: %v", err)
					time.Sleep(1 * time.Second)
					continue
				}

				if frame.Type == FRAME_BODY {
					if err := handler(frame.Payload); err != nil {
						log.Printf("Error handling message: %v", err)
					}
				}
			}
		}
	}()

	return nil
}

// QueueDeclare declares a queue
func (c *AMQPClient) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args map[string]interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	// Queue.Declare method
	method := []byte{
		0x00, 0x32, 0x00, 0x0A, // Queue.Declare
		0x00, 0x00, // reserved-1
	}

	// Add queue name length and name
	method = append(method, byte(len(name)))
	method = append(method, []byte(name)...)

	// Add flags
	var flags byte
	if durable {
		flags |= 0x01
	}
	if autoDelete {
		flags |= 0x02
	}
	if exclusive {
		flags |= 0x04
	}
	if noWait {
		flags |= 0x08
	}
	method = append(method, flags)

	// Add arguments
	argsBytes, err := encodeTable(args)
	if err != nil {
		return fmt.Errorf("failed to encode arguments: %w", err)
	}
	method = append(method, argsBytes...)

	frame := &Frame{
		Type:    FRAME_METHOD,
		Channel: c.channel,
		Size:    uint32(len(method)),
		Payload: method,
		End:     FRAME_END,
	}

	if err := c.sendFrame(frame); err != nil {
		return fmt.Errorf("failed to send queue declare method: %w", err)
	}

	// Wait for response if noWait is false
	if !noWait {
		response, err := c.readFrame()
		if err != nil {
			return fmt.Errorf("failed to read queue declare response: %w", err)
		}

		if response.Type != FRAME_METHOD {
			return fmt.Errorf("unexpected frame type in response: %d", response.Type)
		}

		// Verify Queue.DeclareOk response
		if !bytes.Equal(response.Payload[:4], []byte{0x00, 0x32, 0x00, 0x0B}) {
			return fmt.Errorf("unexpected method in response")
		}
	}

	return nil
}

// ExchangeDeclare declares an exchange
func (c *AMQPClient) ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args map[string]interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	// Exchange.Declare method
	method := []byte{
		0x00, 0x28, 0x00, 0x0A, // Exchange.Declare
		0x00, 0x00, // reserved-1
	}

	// Add exchange name length and name
	method = append(method, byte(len(name)))
	method = append(method, []byte(name)...)

	// Add exchange type length and type
	method = append(method, byte(len(kind)))
	method = append(method, []byte(kind)...)

	// Add flags
	var flags byte
	if durable {
		flags |= 0x01
	}
	if autoDelete {
		flags |= 0x02
	}
	if internal {
		flags |= 0x04
	}
	if noWait {
		flags |= 0x08
	}
	method = append(method, flags)

	// Add arguments
	argsBytes, err := encodeTable(args)
	if err != nil {
		return fmt.Errorf("failed to encode arguments: %w", err)
	}
	method = append(method, argsBytes...)

	frame := &Frame{
		Type:    FRAME_METHOD,
		Channel: c.channel,
		Size:    uint32(len(method)),
		Payload: method,
		End:     FRAME_END,
	}

	if err := c.sendFrame(frame); err != nil {
		return fmt.Errorf("failed to send exchange declare method: %w", err)
	}

	// Wait for response if noWait is false
	if !noWait {
		response, err := c.readFrame()
		if err != nil {
			return fmt.Errorf("failed to read exchange declare response: %w", err)
		}

		if response.Type != FRAME_METHOD {
			return fmt.Errorf("unexpected frame type in response: %d", response.Type)
		}

		// Verify Exchange.DeclareOk response
		if !bytes.Equal(response.Payload[:4], []byte{0x00, 0x28, 0x00, 0x0B}) {
			return fmt.Errorf("unexpected method in response")
		}
	}

	return nil
}

// QueueBind binds a queue to an exchange
func (c *AMQPClient) QueueBind(queue, exchange, routingKey string, noWait bool, args map[string]interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	// Queue.Bind method
	method := []byte{
		0x00, 0x32, 0x00, 0x14, // Queue.Bind
		0x00, 0x00, // reserved-1
	}

	// Add queue name length and name
	method = append(method, byte(len(queue)))
	method = append(method, []byte(queue)...)

	// Add exchange name length and name
	method = append(method, byte(len(exchange)))
	method = append(method, []byte(exchange)...)

	// Add routing key length and name
	method = append(method, byte(len(routingKey)))
	method = append(method, []byte(routingKey)...)

	// Add noWait flag
	if noWait {
		method = append(method, 0x01)
	} else {
		method = append(method, 0x00)
	}

	// Add arguments
	argsBytes, err := encodeTable(args)
	if err != nil {
		return fmt.Errorf("failed to encode arguments: %w", err)
	}
	method = append(method, argsBytes...)

	frame := &Frame{
		Type:    FRAME_METHOD,
		Channel: c.channel,
		Size:    uint32(len(method)),
		Payload: method,
		End:     FRAME_END,
	}

	if err := c.sendFrame(frame); err != nil {
		return fmt.Errorf("failed to send queue bind method: %w", err)
	}

	// Wait for response if noWait is false
	if !noWait {
		response, err := c.readFrame()
		if err != nil {
			return fmt.Errorf("failed to read queue bind response: %w", err)
		}

		if response.Type != FRAME_METHOD {
			return fmt.Errorf("unexpected frame type in response: %d", response.Type)
		}

		// Verify Queue.BindOk response
		if !bytes.Equal(response.Payload[:4], []byte{0x00, 0x32, 0x00, 0x15}) {
			return fmt.Errorf("unexpected method in response")
		}
	}

	return nil
}

// Helper function to encode AMQP table
func encodeTable(args map[string]interface{}) ([]byte, error) {
	if args == nil || len(args) == 0 {
		return []byte{0x00, 0x00, 0x00, 0x00}, nil
	}

	buf := new(bytes.Buffer)
	for key, value := range args {
		// Write key length and key
		buf.WriteByte(byte(len(key)))
		buf.WriteString(key)

		// Write value type and value
		switch v := value.(type) {
		case string:
			buf.WriteByte('S')
			binary.Write(buf, binary.BigEndian, uint32(len(v)))
			buf.WriteString(v)
		case int:
			buf.WriteByte('I')
			binary.Write(buf, binary.BigEndian, int32(v))
		case bool:
			buf.WriteByte('t')
			if v {
				buf.WriteByte(1)
			} else {
				buf.WriteByte(0)
			}
		default:
			return nil, fmt.Errorf("unsupported argument type: %T", value)
		}
	}

	// Prepend table length
	tableBytes := buf.Bytes()
	result := make([]byte, 4+len(tableBytes))
	binary.BigEndian.PutUint32(result, uint32(len(tableBytes)))
	copy(result[4:], tableBytes)

	return result, nil
}
