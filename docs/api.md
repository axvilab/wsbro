# API reference

WSBro implements the [Pusher Channels HTTP API](https://pusher.com/docs/channels/library_auth_reference/rest-api/)
and [WebSocket protocol 7](https://pusher.com/docs/channels/library_auth_reference/pusher-websockets-protocol/).
Any official Pusher server SDK (PHP, Node.js, Go, Python, Ruby, …) works without changes.

## Authentication

Every `/apps/{app_id}/…` request is signed with the app secret, exactly like Pusher:

1. Add `auth_key`, `auth_timestamp` (Unix seconds), `auth_version=1.0` and, for requests with a body,
   `body_md5` (hex MD5 of the body) to the query string.
2. Sign `METHOD\nPATH\nSORTED_QUERY` with HMAC-SHA256 and the secret, where `SORTED_QUERY` is every
   parameter except `auth_signature`, lower-cased keys, sorted, joined with `&`.
3. Add the hex signature as `auth_signature`.

WSBro rejects a request with `401` when the signature is wrong, the key belongs to another app, the
timestamp is more than 10 minutes off, or `body_md5` does not match the body. The SDKs do all of this
for you.

## Trigger an event

```
POST /apps/{app_id}/events
```

```json
{
  "name": "order.shipped",
  "channels": ["orders", "private-App.Models.User.1"],
  "data": "{\"id\": 1}",
  "socket_id": "123.456"
}
```

| Field | Description |
|---|---|
| `name` | Event name, up to 200 characters |
| `channel` / `channels` | One channel, or up to 100 |
| `data` | Payload as a JSON string (a raw JSON object is accepted too), up to `WSBRO_MAX_EVENT_DATA_SIZE` |
| `socket_id` | Optional: this connection does not receive the event (Laravel's `toOthers()`) |

Response: `200 {}`. Errors: `400` invalid event, `413` payload too large, `401` bad signature.

To reach a signed-in user, use the channel `#server-to-user-{user_id}` (SDK: `sendToUser`).

## Trigger a batch

```
POST /apps/{app_id}/batch_events
```

```json
{"batch": [
  {"name": "first", "channel": "orders", "data": "{}"},
  {"name": "second", "channel": "orders", "data": "{}"}
]}
```

Up to 100 events. The batch is validated first and delivered only if every event is valid.

## Channels

```
GET /apps/{app_id}/channels?filter_by_prefix=presence-&info=user_count,subscription_count
```

```json
{"channels": {"presence-chat": {"user_count": 2, "subscription_count": 3}}}
```

`user_count` requires `filter_by_prefix=presence-`, as on Pusher.

```
GET /apps/{app_id}/channels/{channel}?info=subscription_count,user_count
```

```json
{"occupied": true, "subscription_count": 3, "user_count": 2}
```

An unknown channel returns `{"occupied": false}`.

```
GET /apps/{app_id}/channels/{presence_channel}/users
```

```json
{"users": [{"id": "1"}, {"id": "2"}]}
```

## Terminate a user's connections

```
POST /apps/{app_id}/users/{user_id}/terminate_connections
```

Closes every connection signed in as the user (close code 4009, so clients do not reconnect).

## Health and metrics

These endpoints need no authentication.

| Endpoint | Description |
|---|---|
| `GET /health`, `GET /up` | `{"status":"ok"}` for load balancers and orchestrators |
| `GET /metrics` | JSON: connections, peak, channels, events, messages, per-app usage, version, uptime |
| `GET /metrics/prometheus` | The same in Prometheus format, with `app` labels for per-app gauges |

## WebSocket

Connect to `ws://host:6001/app/{app_key}`. The server answers with `pusher:connection_established`
containing the `socket_id` and `activity_timeout` (120 s).

Client to server: `pusher:ping`, `pusher:subscribe`, `pusher:unsubscribe`, `pusher:signin`, `client-*`.

Server to client: `pusher:pong`, `pusher:error`, `pusher_internal:subscription_succeeded`,
`pusher_internal:member_added`, `pusher_internal:member_removed`, `pusher:cache_miss`,
`pusher:signin_success`, and your events.

The server also sends WebSocket pings every minute and drops clients that do not answer within
15 seconds, so dead connections never pile up.

### Error codes

| Code | Meaning | Client behaviour |
|---|---|---|
| 4001 | Unknown app key | does not reconnect |
| 4009 | Unauthorized: bad channel or user signature, or terminated by the server | does not reconnect |
| 4100 | Over capacity, or the client was too slow to receive messages | reconnects after a back-off |
| 4200 | Disconnected from the dashboard | reconnects immediately |
| 4301 | Rate limit exceeded (the message is dropped, the connection stays) | |
| 4302 | Invalid channel name | |
| 4303 | Too many channels for this connection | |
| 4304 | Invalid event (bad JSON, client event not allowed, payload too large) | |
| 1001 | Server is shutting down | reconnects (to another instance) |
