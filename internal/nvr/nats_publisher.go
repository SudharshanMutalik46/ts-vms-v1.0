package nvr

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

type NATSPublisher struct {
	conn       *nats.Conn
	subject    string
	maxRetries int
}

func NewNATSPublisher(conn *nats.Conn, subject string, maxRetries int) *NATSPublisher {
	return &NATSPublisher{
		conn:       conn,
		subject:    subject,
		maxRetries: maxRetries,
	}
}

func (p *NATSPublisher) Publish(event *VmsEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	for i := 0; i <= p.maxRetries; i++ {
		err = p.conn.Publish(p.subject, data)
		if err == nil {
			return nil
		}

		// Backoff
		time.Sleep(time.Duration(i*100) * time.Millisecond)
	}

	return fmt.Errorf("publish failed after %d retries: %w", p.maxRetries, err)
}
