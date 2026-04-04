<p align="center">
  <img src="https://raw.githubusercontent.com/axvilab/wsbro/main/logo.svg" width="120" alt="WSBro">
</p>

<h1 align="center">WSBro</h1>

<p align="center">
  Lightweight, Pusher-compatible WebSocket server built in Go.
  <br>
  <a href="#quick-start">Quick Start</a> &middot;
  <a href="#usage-with-laravel">Laravel Integration</a> &middot;
  <a href="#dashboard">Dashboard</a> &middot;
  <a href="#configuration">Configuration</a>
</p>

<p align="center">
  <img src="https://img.shields.io/github/license/axvilab/wsbro" alt="License">
  <img src="https://img.shields.io/github/go-mod/go-version/axvilab/wsbro" alt="Go Version">
</p>

---

## Why WSBro?

WSBro is a self-hosted WebSocket server that speaks the Pusher protocol. If your app already uses Pusher, Laravel Echo, or any Pusher-compatible SDK — just point it to WSBro and everything works. No code changes required.

**What makes it different:**

- **Single binary** — no Node.js, no PHP, no runtime dependencies. Download, run, done.
- **Built for Go** — goroutines handle thousands of concurrent connections with minimal memory.
- **Dashboard included** — real-time monitoring out of the box, no extra setup.
- **Easy to deploy** — one binary or one Docker image on your VPS and you're live.

## Features

- **Pusher Protocol 7** compatible (`pusher-js`, `laravel-echo`, all Pusher SDKs)
- Public, Private & Presence channels
- Client events (whisper)
- Batch event broadcasting
- HMAC request authentication
- Rate limiting (per connection)
- Max connections limit
- Webhooks (per app or global)
- Horizontal scaling via Redis or NATS adapters
- Built-in real-time dashboard with auth
- JSON and Prometheus metrics endpoints
- Automatic empty channel cleanup
- Graceful shutdown
- Security: crypto/rand socket IDs, message size limits, channel limits per connection
- Configuration via environment variables

## Quick Start

### Binary

```bash
go install github.com/axvilab/wsbro@latest
wsbro
```

### Docker

```bash
docker run -p 6001:6001 axvi/wsbro
```

### Docker Compose

```bash
git clone https://github.com/axvilab/wsbro.git
cd wsbro
docker compose up
```

Server starts on `http://localhost:6001`
Dashboard at `http://localhost:6001/dashboard`

## Usage with Laravel

Update your `.env`:

```env
BROADCAST_DRIVER=pusher
PUSHER_APP_ID=app-id
PUSHER_APP_KEY=app-key
PUSHER_APP_SECRET=app-secret
PUSHER_HOST=localhost
PUSHER_PORT=6001
PUSHER_SCHEME=http
```

Update `config/broadcasting.php`:

```php
'pusher' => [
    'driver' => 'pusher',
    'key' => env('PUSHER_APP_KEY'),
    'secret' => env('PUSHER_APP_SECRET'),
    'app_id' => env('PUSHER_APP_ID'),
    'options' => [
        'host' => env('PUSHER_HOST', '127.0.0.1'),
        'port' => env('PUSHER_PORT', 6001),
        'scheme' => env('PUSHER_SCHEME', 'http'),
        'useTLS' => false,
    ],
],
```

Frontend with Laravel Echo:

```javascript
import Echo from 'laravel-echo';
import Pusher from 'pusher-js';

window.Pusher = Pusher;

window.Echo = new Echo({
    broadcaster: 'pusher',
    key: import.meta.env.VITE_PUSHER_APP_KEY,
    wsHost: import.meta.env.VITE_PUSHER_HOST,
    wsPort: import.meta.env.VITE_PUSHER_PORT,
    forceTLS: false,
    enabledTransports: ['ws'],
    cluster: '',
});
```

## Configuration

All configuration is done via environment variables.

| Variable | Default | Description |
|----------|---------|-------------|
| `WSBRO_PORT` | `6001` | Server port |
| `WSBRO_APP_ID` | `app-id` | Application ID |
| `WSBRO_APP_KEY` | `app-key` | Application key |
| `WSBRO_APP_SECRET` | `app-secret` | Application secret |
| `WSBRO_APPS` | — | Multiple apps as JSON array |
| `WSBRO_ADAPTER` | `memory` | Adapter: `memory`, `redis`, `nats` |
| `WSBRO_REDIS_ADDR` | `localhost:6379` | Redis address |
| `WSBRO_REDIS_PASSWORD` | — | Redis password |
| `WSBRO_NATS_URL` | `nats://localhost:4222` | NATS URL |
| `WSBRO_DASHBOARD_PASSWORD` | — | Dashboard password (empty = no auth) |
| `WSBRO_MAX_MESSAGES_PER_SECOND` | `100` | Rate limit per connection |
| `WSBRO_MAX_CONNECTIONS` | `0` | Max connections (0 = unlimited) |
| `WSBRO_WEBHOOK_URL` | — | Webhook URL for events |
| `WSBRO_ALLOWED_ORIGINS` | `*` | CORS allowed origins |
| `WSBRO_MAX_MESSAGE_SIZE` | `65536` | Max WebSocket message size (bytes) |
| `WSBRO_MAX_CHANNELS_PER_CONN` | `100` | Max channel subscriptions per connection |
| `WSBRO_MAX_EVENT_DATA_SIZE` | `10240` | Max HTTP event body size (bytes) |

### Multiple Apps

```bash
WSBRO_APPS='[{"id":"app-1","key":"key-1","secret":"secret-1"},{"id":"app-2","key":"key-2","secret":"secret-2"}]'
```

## Dashboard

Built-in real-time dashboard with:

- Live connection and channel metrics
- Connection history graph
- Active channels with search, type filtering, and pagination
- Password protection

Access at `http://localhost:6001/dashboard`

## API

### Broadcast Event

```
POST /apps/{app_id}/events
```

```json
{
  "name": "order.created",
  "channel": "orders",
  "data": "{\"id\": 1}"
}
```

### Batch Events

```
POST /apps/{app_id}/batch_events
```

```json
{
  "batch": [
    {"name": "order.created", "channel": "orders", "data": "{\"id\": 1}"},
    {"name": "notification", "channel": "admin", "data": "{\"text\": \"New order\"}"}
  ]
}
```

### Channel Info

```
GET /apps/{app_id}/channels
GET /apps/{app_id}/channels?filter_by_prefix=private-
GET /apps/{app_id}/channels/{channel_name}
GET /apps/{app_id}/channels/{channel_name}/users
```

### Metrics

```
GET /metrics
GET /metrics/prometheus
```

## Adapters

| Adapter | Use Case |
|---------|----------|
| `memory` | Single instance (default) |
| `redis` | Multi-instance with Redis pub/sub |
| `nats` | Multi-instance with NATS |

```bash
# Redis
WSBRO_ADAPTER=redis WSBRO_REDIS_ADDR=localhost:6379 wsbro

# NATS
WSBRO_ADAPTER=nats WSBRO_NATS_URL=nats://localhost:4222 wsbro
```

## Development

```bash
git clone https://github.com/axvilab/wsbro.git
cd wsbro
go run .
```

```bash
# Run tests
go test ./server/ -v

# Build binary
go build -o wsbro .
```

## Documentation

- [Channels Guide](docs/channels.md) — public, private, presence channels and client events
- [API Reference](docs/api.md) — HTTP endpoints, WebSocket protocol, authentication
- [Deployment](docs/deployment.md) — systemd, Docker, Nginx, Caddy, FrankenPHP
- [Adapters](docs/adapters.md) — Memory, Redis, NATS setup and comparison
- [Webhooks](docs/webhooks.md) — event notifications to your backend

## License

[MIT](LICENSE)