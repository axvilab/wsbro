# Channels

Channel type is decided by the name prefix, as on Pusher.

| Prefix | Type | Who can subscribe |
|---|---|---|
| *(none)* | public | anyone with the app key |
| `private-` | private | clients with a token signed by your backend |
| `private-encrypted-` | encrypted | as private; payloads are end-to-end encrypted by your backend |
| `presence-` | presence | as private, plus the member list is shared |
| `cache-`, `private-cache-`, `presence-cache-` | cache | as the type above; the last event is replayed to new subscribers |

Channel names may contain letters, digits and `_ - = @ , . ; :`, up to 200 characters.
Laravel's `App.Models.User.1` style works as is.

## Public

```js
Echo.channel('orders').listen('.order.shipped', (e) => console.log(e));
```

## Private

The client asks your backend for a signature (Laravel: `/broadcasting/auth`, which runs your
`routes/channels.php`), then sends it to WSBro. WSBro checks it with the app secret:

```
auth = app_key + ":" + HMAC_SHA256(secret, socket_id + ":" + channel_name)
```

A wrong or missing signature returns error `4009` and the subscription is refused.

## Presence

Like private channels, but the signed `channel_data` carries the user:

```json
{"user_id": "42", "user_info": {"name": "Ann"}}
```

`user_id` may be a string or a number. On subscribe, the client receives every member. Others receive
`member_added` when a user arrives and `member_removed` when the user's **last** connection leaves:
a user with three open tabs is one member, and closing one tab does not make them "leave".

```js
Echo.join('chat')
    .here((users) => {})
    .joining((user) => {})
    .leaving((user) => {});
```

## Client events (whisper)

Clients may send `client-*` events to other subscribers, without going through your backend:

```js
Echo.private('chat').whisper('typing', { name: 'Ann' });
```

Rules, as on Pusher:

- only on private and presence channels;
- the sender must be subscribed to the channel;
- the sender does not receive its own event;
- on presence channels the event carries the sender's `user_id`;
- the rate limit (`WSBRO_MAX_MESSAGES_PER_SECOND`) applies.

## Cache channels

A cache channel remembers its last event for 30 minutes. A new subscriber receives it right after
subscribing, or `pusher:cache_miss` when there is none. Useful for "current state" channels, such as
prices or a build status, where a late client should not wait for the next update.

## User channels

After `pusher:signin`, a connection is bound to a user ID. The server can then:

- send an event to every connection of that user: `$pusher->sendToUser($userId, 'event', $data)`;
- disconnect the user everywhere: `$pusher->terminateUserConnections($userId)`.

Internally this uses the `#server-to-user-{id}` channel, which clients cannot subscribe to directly.
