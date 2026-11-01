# Webhooks

WSBro tells your backend when channels become occupied or empty and when users join or leave presence
channels. Webhooks use the [Pusher format](https://pusher.com/docs/channels/server_api/webhooks/), so
the official SDKs can verify them.

## Setup

Per app (recommended):

```bash
WSBRO_APPS='[{"id":"shop","key":"shop-key","secret":"shop-secret","webhook_url":"https://shop.example.com/wsbro/webhook"}]'
```

Or for every app without its own URL:

```bash
WSBRO_WEBHOOK_URL=https://example.com/wsbro/webhook
```

## Events

| Event | When | Fields |
|---|---|---|
| `channel_occupied` | the first connection subscribes | `channel` |
| `channel_vacated` | the last connection leaves | `channel` |
| `member_added` | a user joins a presence channel (first connection of that user) | `channel`, `user_id` |
| `member_removed` | a user leaves a presence channel (last connection of that user) | `channel`, `user_id` |

## Request

```http
POST /wsbro/webhook
Content-Type: application/json
X-Pusher-Key: shop-key
X-Pusher-Signature: 3c1a…  (hex HMAC-SHA256 of the body with the app secret)

{"time_ms": 1793565664010, "events": [{"name": "member_added", "channel": "presence-chat", "user_id": "42"}]}
```

## Verifying in Laravel

```php
$webhook = Broadcast::connection()->getPusher()->webhook(
    ['X-Pusher-Key' => $request->header('X-Pusher-Key'), 'X-Pusher-Signature' => $request->header('X-Pusher-Signature')],
    $request->getContent(),
);

foreach ($webhook->get_events() as $event) {
    // $event->name, $event->channel, $event->user_id
}
```

This exact code runs in the [end-to-end tests](../examples/laravel/files/app/Http/Controllers/WebhookController.php).

## Delivery

- Webhooks of one app are sent one at a time, in order, with a 5 second timeout.
- A response with status 2xx counts as delivered. Failures are logged and not retried.
- When the backend is slow and 1000 webhooks are waiting, new ones are dropped with a warning,
  so a slow backend can never slow down WebSocket traffic.
- With several WSBro instances, each instance reports the channels of its own connections.
