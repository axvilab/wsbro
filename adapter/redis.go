package adapter

import (
	"context"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const redisPrefix = "wsbro:"

// RedisAdapter uses Redis Pub/Sub for cross-instance message delivery.
type RedisAdapter struct {
	client *redis.Client
	ctx    context.Context
	cancel context.CancelFunc
}

// NewRedisAdapter connects to Redis and verifies the connection with a PING.
func NewRedisAdapter(addr, password string, db int) (*RedisAdapter, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithCancel(context.Background())

	if err := client.Ping(ctx).Err(); err != nil {
		cancel()
		return nil, err
	}

	log.Info().Str("addr", addr).Msg("Redis adapter connected")

	return &RedisAdapter{
		client: client,
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

func (r *RedisAdapter) Publish(channel string, message []byte) error {
	return r.client.Publish(r.ctx, redisPrefix+channel, message).Err()
}

func (r *RedisAdapter) Subscribe(handler func(channel string, message []byte)) error {
	pubsub := r.client.PSubscribe(r.ctx, redisPrefix+"*")

	go func() {
		defer func() { _ = pubsub.Close() }()
		ch := pubsub.Channel()

		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				channelName := msg.Channel[len(redisPrefix):]
				handler(channelName, []byte(msg.Payload))
			case <-r.ctx.Done():
				return
			}
		}
	}()

	return nil
}

func (r *RedisAdapter) Close() error {
	r.cancel()
	return r.client.Close()
}
