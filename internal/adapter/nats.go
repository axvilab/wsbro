package adapter

import (
	"github.com/nats-io/nats.go"
)

const natsSubject = "wsbro.broadcast"

// NATS relays messages through a single NATS subject.
type NATS struct {
	conn *nats.Conn
	sub  *nats.Subscription
}

// NewNATS connects to a NATS server.
func NewNATS(url string) (*NATS, error) {
	nc, err := nats.Connect(url, nats.Name("wsbro"), nats.MaxReconnects(-1))
	if err != nil {
		return nil, err
	}
	return &NATS{conn: nc}, nil
}

func (n *NATS) Name() string { return "nats" }

func (n *NATS) Publish(payload []byte) error {
	return n.conn.Publish(natsSubject, payload)
}

func (n *NATS) Subscribe(handler func(payload []byte)) error {
	sub, err := n.conn.Subscribe(natsSubject, func(msg *nats.Msg) {
		handler(msg.Data)
	})
	if err != nil {
		return err
	}
	n.sub = sub
	// Make sure the server registered the subscription before we start publishing.
	return n.conn.Flush()
}

func (n *NATS) Close() error {
	if n.sub != nil {
		_ = n.sub.Unsubscribe()
	}
	n.conn.Close()
	return nil
}
