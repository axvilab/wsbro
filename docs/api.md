# API Reference

WSBro implements [Pusher Protocol 7](https://pusher.com/docs/channels/library_auth_reference/pusher-websockets-protocol/) — the current standard used by `pusher-js` v7/v8 and `laravel-echo`.

All HTTP API endpoints require HMAC authentication via query parameters.

## Limits

| Limit | Value | Description |
|-------|-------|-------------|
| Event data size | 10 KB | Maximum HTTP request body size |
| WebSocket message | 64 KB | Maximum incoming WebSocket message size |
| Channels per connection | 100 | Maximum channel subscriptions per WebSocket connection |
| Presence channel_data | Required | Must include `user_id` field |

## Authentication

Every request must include these query parameters:

| Parameter | Description |
|-----------|-------------|
| `auth_key` | Your app key |
| `auth_timestamp` | Current Unix timestamp |
| `auth_version` | Always `1.0` |
| `auth_signature` | HMAC-SHA256 signature |

The signature is computed as:

```
HMAC-SHA256(
  app_secret,
  "POST\n/apps/{app_id}/events\nauth_key={key}&auth_timestamp={ts}&auth_version=1.0&body_md5={md5}"
)
```

Where `body_md5` is the MD5 hex digest of the request body.

Laravel's Pusher SDK handles this automatically. For manual requests, see examples below.

## Endpoints

### POST /apps/{app_id}/events

Broadcast an event to one or more channels.

**Single channel:**

```bash
curl -X POST "http://localhost:6001/apps/app-id/events?\
auth_key=app-key&\
auth_timestamp=$(date +%s)&\
auth_version=1.0&\
auth_signature=COMPUTED_SIGNATURE" \
  -H "Content-Type: application/json" \
  -d '{"name":"order.created","channel":"orders","data":"{\"id\":1}"}'
```

**Multiple channels:**

```json
{
  "name": "notification",
  "channels": ["orders", "admin", "support"],
  "data": "{\"message\": \"New order!\"}"
}
```

**Exclude sender:**

```json
{
  "name": "message.sent",
  "channel": "private-chat",
  "data": "{\"text\": \"Hello\"}",
  "socket_id": "123.456"
}
```

**Response:**

```json
{"status": "ok"}
```

### POST /apps/{app_id}/batch_events

Broadcast multiple events in a single request.

```json
{
  "batch": [
    {"name": "order.created", "channel": "orders", "data": "{\"id\": 1}"},
    {"name": "notification", "channel": "admin", "data": "{\"text\": \"New order\"}"}
  ]
}
```

**Response:**

```json
{"status": "ok"}
```

### GET /apps/{app_id}/channels

List all active channels.

**Query parameters:**

| Parameter | Description |
|-----------|-------------|
| `filter_by_prefix` | Filter channels by name prefix (e.g. `private-`, `presence-`) |

**Example:** `GET /apps/{app_id}/channels?filter_by_prefix=presence-`

**Response:**

```json
{
  "channels": {
    "orders": {
      "subscription_count": 5,
      "occupied": true
    },
    "private-chat": {
      "subscription_count": 2,
      "occupied": true
    }
  }
}
```

### GET /apps/{app_id}/channels/{channel_name}

Get info about a specific channel.

**Response (public/private):**

```json
{
  "occupied": true,
  "subscription_count": 5
}
```

**Response (presence):**

```json
{
  "occupied": true,
  "subscription_count": 5,
  "user_count": 3
}
```

### GET /apps/{app_id}/channels/{channel_name}/users

List users in a presence channel. Only works for `presence-` channels.

**Response:**

```json
{
  "users": [
    {"id": "1"},
    {"id": "2"},
    {"id": "3"}
  ]
}
```

### GET /metrics

Server metrics as JSON. No authentication required.

**Response:**

```json
{
  "connections": 25,
  "peak_connections": 100,
  "total_events": 1234,
  "total_messages": 5678,
  "channels": 10,
  "version": "0.1.0"
}
```

### GET /metrics/prometheus

Server metrics in Prometheus text format. Use this endpoint as a scrape target in Prometheus/Grafana.

**Response:**

```
# HELP wsbro_connections Current WebSocket connections
# TYPE wsbro_connections gauge
wsbro_connections 25
# HELP wsbro_peak_connections Peak WebSocket connections
# TYPE wsbro_peak_connections gauge
wsbro_peak_connections 100
# HELP wsbro_channels Active channels
# TYPE wsbro_channels gauge
wsbro_channels 10
# HELP wsbro_events_total Total broadcast events
# TYPE wsbro_events_total counter
wsbro_events_total 1234
# HELP wsbro_messages_total Total messages sent
# TYPE wsbro_messages_total counter
wsbro_messages_total 5678
```

## WebSocket Protocol

Clients connect via WebSocket:

```
ws://localhost:6001/app/{app_key}
```

### Connection

After connecting, the server sends:

```json
{
  "event": "pusher:connection_established",
  "data": "{\"socket_id\":\"123.456\",\"activity_timeout\":120}"
}
```

### Subscribe

```json
{"event": "pusher:subscribe", "data": {"channel": "orders"}}
```

For private/presence channels, include `auth`:

```json
{
  "event": "pusher:subscribe",
  "data": {
    "channel": "private-orders",
    "auth": "app-key:SIGNATURE"
  }
}
```

### Unsubscribe

```json
{"event": "pusher:unsubscribe", "data": {"channel": "orders"}}
```

### Ping/Pong

```json
{"event": "pusher:ping"}
```

Server responds with:

```json
{"event": "pusher:pong", "data": "null"}
```