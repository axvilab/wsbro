package adapter

import (
	"context"

	"github.com/redis/go-redis/v9"
)

const redisChannel = "wsbro:broadcast"

// Redis relays messages through a single Redis Pub/Sub channel.
type Redis struct {
	client *redis.Client
	ctx    context.Context
	cancel context.CancelFunc
}

// NewRedis connects to Redis and verifies the connection.
func NewRedis(addr, password string, db int) (*Redis, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := client.Ping(ctx).Err(); err != nil {
		cancel()
		_ = client.Close()
		return nil, err
	}

	return &Redis{client: client, ctx: ctx, cancel: cancel}, nil
}

func (r *Redis) Name() string { return "redis" }

func (r *Redis) Publish(payload []byte) error {
	return r.client.Publish(r.ctx, redisChannel, payload).Err()
}

func (r *Redis) Subscribe(handler func(payload []byte)) error {
	pubsub := r.client.Subscribe(r.ctx, redisChannel)
	// Wait for the subscription to be confirmed so no early message is lost.
	if _, err := pubsub.Receive(r.ctx); err != nil {
		_ = pubsub.Close()
		return err
	}

	go func() {
		defer func() { _ = pubsub.Close() }()
		messages := pubsub.Channel()
		for {
			select {
			case <-r.ctx.Done():
				return
			case msg, ok := <-messages:
				if !ok {
					return
				}
				handler([]byte(msg.Payload))
			}
		}
	}()
	return nil
}

func (r *Redis) Close() error {
	r.cancel()
	return r.client.Close()
}
