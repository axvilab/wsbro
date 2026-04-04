package adapter

import (
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

const natsPrefix = "wsbro."

// NATSAdapter uses NATS subjects for cross-instance message delivery.
type NATSAdapter struct {
	conn *nats.Conn
	subs []*nats.Subscription
}

// NewNATSAdapter connects to a NATS server at the given URL.
func NewNATSAdapter(url string) (*NATSAdapter, error) {
	nc, err := nats.Connect(url)
	if err != nil {
		return nil, err
	}

	log.Info().Str("url", url).Msg("NATS adapter connected")

	return &NATSAdapter{
		conn: nc,
	}, nil
}

func (n *NATSAdapter) Publish(channel string, message []byte) error {
	return n.conn.Publish(natsPrefix+channel, message)
}

func (n *NATSAdapter) Subscribe(handler func(channel string, message []byte)) error {
	sub, err := n.conn.Subscribe(natsPrefix+"*", func(msg *nats.Msg) {
		channelName := msg.Subject[len(natsPrefix):]
		handler(channelName, msg.Data)
	})
	if err != nil {
		return err
	}

	n.subs = append(n.subs, sub)
	return nil
}

func (n *NATSAdapter) Close() error {
	for _, sub := range n.subs {
		_ = sub.Unsubscribe()
	}
	n.conn.Close()
	return nil
}
