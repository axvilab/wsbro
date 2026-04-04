# Channels

WSBro supports three types of channels, following the Pusher protocol.

## Public Channels

Any client can subscribe without authentication.

```javascript
const channel = pusher.subscribe('orders');

channel.bind('order.created', (data) => {
    console.log('New order:', data);
});
```

**Use cases:** public feeds, live scores, stock tickers, notifications visible to everyone.

## Private Channels

Channel name must start with `private-`. Requires authentication — the client must obtain an auth signature from your backend before subscribing.

```javascript
const channel = pusher.subscribe('private-user-42');

channel.bind('message', (data) => {
    console.log('Private message:', data);
});
```

### How authentication works

1. Client connects to WSBro, receives `socket_id`
2. Client asks to subscribe to `private-user-42`
3. `pusher-js` sends a POST request to your backend (`/broadcasting/auth` in Laravel)
4. Your backend verifies the user has access to this channel
5. Your backend generates HMAC signature: `app_key:HMAC-SHA256(app_secret, "socket_id:channel_name")`
6. Client sends the signature to WSBro
7. WSBro verifies the signature and grants access

In Laravel, this is handled automatically by the `BroadcastServiceProvider` and channel authorization in `routes/channels.php`:

```php
Broadcast::channel('user-{id}', function ($user, $id) {
    return $user->id === (int) $id;
});
```

## Presence Channels

Channel name must start with `presence-`. Like private channels but with member tracking — every subscriber knows who else is in the channel.

```javascript
const channel = pusher.subscribe('presence-chat-room');

// Current members when you join
channel.bind('pusher:subscription_succeeded', (members) => {
    console.log('Members:', members.count);
    members.each((member) => {
        console.log(member.id, member.info);
    });
});

// Someone joins
channel.bind('pusher:member_added', (member) => {
    console.log('Joined:', member.info.name);
});

// Someone leaves
channel.bind('pusher:member_removed', (member) => {
    console.log('Left:', member.info.name);
});
```

In Laravel, return user data from the channel authorization:

```php
Broadcast::channel('chat-room', function ($user) {
    return ['id' => $user->id, 'name' => $user->name];
});
```

**Use cases:** chat rooms, collaborative editing, "who's online" indicators.

**Requirements:**
- `channel_data` is required and must include `user_id`
- `user_id` must be a string (even if numeric in your database)

## Client Events (Whisper)

Clients can send events directly to other clients without going through your backend. Event names must start with `client-`. Only works on private and presence channels.

```javascript
// Send
channel.trigger('client-typing', { user: 'Alice' });

// Receive (other clients)
channel.bind('client-typing', (data) => {
    console.log(data.user + ' is typing...');
});
```

In Laravel Echo:

```javascript
channel.whisper('typing', { user: 'Alice' });

channel.listenForWhisper('typing', (data) => {
    console.log(data.user + ' is typing...');
});
```

**Use cases:** typing indicators, cursor positions, drawing on shared canvas.

## Broadcasting Events from Backend

Your backend sends events to channels via HTTP API:

```bash
POST /apps/{app_id}/events

{
  "name": "order.created",
  "channel": "orders",
  "data": "{\"id\": 1, \"total\": 99.99}"
}
```

Send to multiple channels at once:

```json
{
  "name": "notification",
  "channels": ["orders", "admin"],
  "data": "{\"message\": \"New order!\"}"
}
```

Exclude the sender (so they don't receive their own event):

```json
{
  "name": "message.sent",
  "channel": "private-chat",
  "data": "{\"text\": \"Hello\"}",
  "socket_id": "123.456"
}
```

In Laravel, this happens automatically when you call `broadcast()`:

```php
broadcast(new OrderCreated($order));
broadcast(new OrderCreated($order))->toOthers(); // excludes sender
```