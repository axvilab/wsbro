# Configuration

WSBro is configured with environment variables only. Invalid values stop the server at startup with
a clear message, instead of running half-configured.

## Applications

One server can host several applications. Each has its own channels, limits and webhooks, and nothing
leaks between them: a channel named `orders` in one app is unrelated to `orders` in another.

**Single app:**

| Variable | Default | Description |
|---|---|---|
| `WSBRO_APP_ID` | `app-id` | App ID, used in the HTTP API path (`/apps/{id}/events`) |
| `WSBRO_APP_KEY` | `app-key` | Public key, used by clients (`/app/{key}`) |
| `WSBRO_APP_SECRET` | `app-secret` | Secret for signing API requests and channel auth |

The defaults are for local development. WSBro logs a warning when the default secret is used.

**Several apps** with `WSBRO_APPS` (a JSON array; it replaces the variables above):

```bash
WSBRO_APPS='[
  {"id": "shop", "key": "shop-key", "secret": "shop-secret",
   "max_connections": 20000, "max_messages_per_second": 50,
   "allowed_origins": "shop.example.com", "webhook_url": "https://shop.example.com/wsbro/webhook"},
  {"id": "blog", "key": "blog-key", "secret": "blog-secret"}
]'
```

| Field | Required | Description |
|---|---|---|
| `id`, `key`, `secret` | yes | Must be unique across apps |
| `max_connections` | no | Connection limit for this app (0 = only the server limit applies) |
| `max_messages_per_second` | no | Overrides `WSBRO_MAX_MESSAGES_PER_SECOND` |
| `allowed_origins` | no | Overrides `WSBRO_ALLOWED_ORIGINS` |
| `webhook_url` | no | Overrides `WSBRO_WEBHOOK_URL` |

## Server

| Variable | Default | Description |
|---|---|---|
| `WSBRO_HOST` | `0.0.0.0` | Address to listen on |
| `WSBRO_PORT` | `6001` | Port to listen on |
| `WSBRO_TLS_CERT`, `WSBRO_TLS_KEY` | | Serve TLS directly (both required). Usually a reverse proxy does this |
| `WSBRO_ALLOWED_ORIGINS` | `*` | Comma-separated sites allowed to open WebSockets: `example.com,*.example.com`. Scheme is optional |
| `WSBRO_DASHBOARD_PASSWORD` | | Enables the [dashboard](dashboard.md). Without it the dashboard is off |
| `WSBRO_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `WSBRO_LOG_FORMAT` | `console` | `console` for humans, `json` for log collectors |

## Limits

| Variable | Default | Description |
|---|---|---|
| `WSBRO_MAX_CONNECTIONS` | `0` | Server-wide connection limit, 0 = unlimited. Extra clients get error 4100 and retry later |
| `WSBRO_MAX_MESSAGES_PER_SECOND` | `100` | Messages a client may send per second (pings, subscriptions, whispers) |
| `WSBRO_MAX_CHANNELS_PER_CONN` | `100` | Channels one connection may subscribe to |
| `WSBRO_MAX_MESSAGE_SIZE` | `65536` | Largest WebSocket message a client may send, in bytes |
| `WSBRO_MAX_EVENT_DATA_SIZE` | `10240` | Largest event payload, in bytes (Pusher's limit is 10 KB) |

## Scaling

| Variable | Default | Description |
|---|---|---|
| `WSBRO_ADAPTER` | `memory` | `memory` (one instance), `redis` or `nats`. See [scaling](scaling.md) |
| `WSBRO_REDIS_ADDR` | `localhost:6379` | Redis address |
| `WSBRO_REDIS_PASSWORD` | | Redis password |
| `WSBRO_REDIS_DB` | `0` | Redis database |
| `WSBRO_NATS_URL` | `nats://localhost:4222` | NATS server URL |

## Webhooks

| Variable | Default | Description |
|---|---|---|
| `WSBRO_WEBHOOK_URL` | | Webhook URL for apps that do not set their own. See [webhooks](webhooks.md) |

## Go runtime

WSBro is a Go program, so the standard runtime variables work too. On a busy server, set a soft memory
limit a bit below the container limit, so the garbage collector works harder before memory runs out:

```bash
GOMEMLIMIT=3GiB
```
