# Webhooks

WSBro can notify your backend about connection and channel events via HTTP POST requests.

## Setup

Global webhook URL (for all apps):

```bash
WSBRO_WEBHOOK_URL=https://your-app.com/wsbro/webhook
```

Per-app webhook URL (overrides global):

```bash
WSBRO_APPS='[{"id":"app-1","key":"key-1","secret":"secret-1","webhook_url":"https://app1.com/webhook"}]'
```

If neither is set, no webhooks are sent. App-level URL takes priority over global.

## Events

### connection_established

Fired when a client connects.

```json
{
  "event": "connection_established",
  "socket_id": "123.456",
  "timestamp": 1712250000
}
```

### connection_closed

Fired when a client disconnects.

```json
{
  "event": "connection_closed",
  "socket_id": "123.456",
  "timestamp": 1712250000
}
```

### channel_occupied

Fired when the first client subscribes to a channel (channel becomes active).

```json
{
  "event": "channel_occupied",
  "channel": "orders",
  "timestamp": 1712250000
}
```

### channel_vacated

Fired when the last client unsubscribes from a channel (channel becomes empty).

```json
{
  "event": "channel_vacated",
  "channel": "orders",
  "timestamp": 1712250000
}
```

### member_added

Fired when a user joins a presence channel.

```json
{
  "event": "member_added",
  "channel": "presence-chat",
  "socket_id": "123.456",
  "data": {
    "user_id": "1",
    "user_info": {"name": "Alice"}
  },
  "timestamp": 1712250000
}
```

### member_removed

Fired when a user leaves a presence channel.

```json
{
  "event": "member_removed",
  "channel": "presence-chat",
  "socket_id": "123.456",
  "data": {
    "user_id": "1",
    "user_info": {"name": "Alice"}
  },
  "timestamp": 1712250000
}
```

## Laravel Example

```php
// routes/web.php
Route::post('/wsbro/webhook', function (Request $request) {
    $event = $request->input('event');
    $socketId = $request->input('socket_id');

    match ($event) {
        'connection_established' => Log::info("Client connected: $socketId"),
        'connection_closed' => Log::info("Client disconnected: $socketId"),
        'channel_occupied' => Log::info("Channel active: " . $request->input('channel')),
        'channel_vacated' => Log::info("Channel empty: " . $request->input('channel')),
        'member_added' => Log::info("Member joined: " . $request->input('data.user_id')),
        'member_removed' => Log::info("Member left: " . $request->input('data.user_id')),
    };

    return response()->json(['ok' => true]);
});
```

## Notes

- Webhooks are sent asynchronously — they don't block WebSocket operations.
- Timeout is 5 seconds per request.
- Failed deliveries are logged but not retried. For reliability, consider using a queue in your backend.