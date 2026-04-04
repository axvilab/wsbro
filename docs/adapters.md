# Adapters

Adapters handle message synchronization between multiple WSBro instances. If you run a single instance, the default memory adapter is all you need.

## Memory (default)

No configuration needed. Messages are delivered locally within the same process.

```bash
WSBRO_ADAPTER=memory
```

**When to use:** Single server deployments. Handles thousands of connections on a small VPS.

## Redis

Uses Redis Pub/Sub to synchronize messages across multiple WSBro instances.

```bash
WSBRO_ADAPTER=redis
WSBRO_REDIS_ADDR=localhost:6379
WSBRO_REDIS_PASSWORD=your-password  # optional
```

**When to use:** When you need more than one WSBro instance behind a load balancer.

### Setup

1. Install Redis:

```bash
# Docker
docker run -d --name redis -p 6379:6379 redis:7-alpine

# Ubuntu
sudo apt install redis-server
```

2. Configure WSBro:

```bash
WSBRO_ADAPTER=redis WSBRO_REDIS_ADDR=redis:6379 wsbro
```

3. Run multiple instances:

```yaml
services:
  wsbro-1:
    image: axvi/wsbro
    ports:
      - "6001:6001"
    environment:
      WSBRO_ADAPTER: redis
      WSBRO_REDIS_ADDR: redis:6379

  wsbro-2:
    image: axvi/wsbro
    ports:
      - "6002:6001"
    environment:
      WSBRO_ADAPTER: redis
      WSBRO_REDIS_ADDR: redis:6379

  redis:
    image: redis:7-alpine
```

Put a load balancer (Nginx, HAProxy) in front with sticky sessions or IP hash.

### Redis Channel Prefix

All Redis channels are prefixed with `wsbro:` to avoid conflicts with other applications using the same Redis instance.

## NATS

Uses NATS messaging for cross-instance synchronization. NATS is lighter than Redis and designed specifically for messaging.

```bash
WSBRO_ADAPTER=nats
WSBRO_NATS_URL=nats://localhost:4222
```

**When to use:** When you want a lightweight alternative to Redis, or already use NATS in your infrastructure.

### Setup

1. Install NATS:

```bash
# Docker
docker run -d --name nats -p 4222:4222 nats:latest

# Binary
curl -L https://github.com/nats-io/nats-server/releases/latest/download/nats-server-linux-amd64.zip -o nats.zip
unzip nats.zip
./nats-server
```

2. Configure WSBro:

```bash
WSBRO_ADAPTER=nats WSBRO_NATS_URL=nats://localhost:4222 wsbro
```

### NATS Subject Prefix

All NATS subjects are prefixed with `wsbro.` to avoid conflicts.

## Choosing an Adapter

| | Memory | Redis | NATS |
|---|--------|-------|------|
| Instances | 1 | Multiple | Multiple |
| External dependency | None | Redis | NATS |
| Latency | None | ~1ms | ~0.5ms |
| Best for | Single VPS | Most production setups | High-throughput messaging |