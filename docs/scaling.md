# Scaling

## Start with one instance

One WSBro process held **100,000 concurrent connections in 3.3 GB of RAM** in our
[benchmarks](benchmarks.md), and delivers 160,000 messages per second with a p99 latency of 3 ms.
Most applications never need more than one instance; scale vertically first.

What to tune on a big single instance:

- **File descriptors**: every connection is a socket. Raise the limit (`ulimit -n 1048576`,
  `LimitNOFILE=1048576` in systemd, `--ulimit nofile=1048576:1048576` in Docker).
- **Memory**: plan about 35 KB per connection, and set `GOMEMLIMIT` a bit below the available memory.
- **Limits**: set `WSBRO_MAX_CONNECTIONS` so a traffic spike degrades gracefully (clients get 4100
  and retry with back-off) instead of exhausting the machine.

## Several instances

For high availability, or more connections than one machine holds, run several instances behind a
load balancer and connect them with Redis or NATS. Each WebSocket stays on the instance it connected
to; the adapter relays every event to the other instances.

```bash
WSBRO_ADAPTER=redis WSBRO_REDIS_ADDR=redis:6379 wsbro
WSBRO_ADAPTER=nats  WSBRO_NATS_URL=nats://nats:4222 wsbro
```

| | Memory | Redis | NATS |
|---|---|---|---|
| Instances | 1 | many | many |
| Extra service | none | Redis (Pub/Sub only, nothing is stored) | NATS |
| Extra latency (measured) | — | ~0.3 ms p50 | ~0.9 ms p50 |
| Pick it when | one server | you already run Redis (Laravel usually does) | you already run NATS |

Both adapters were tested end to end: 20,000 clients on instance B, 2,000 events triggered on
instance A, 200,000 of 200,000 messages delivered, no duplicates.

### What is shared and what is not

| | Across instances |
|---|---|
| Events from the HTTP API (`broadcast()`, `triggerBatch`, `sendToUser`) | ✅ delivered everywhere, exactly once |
| Client events (whispers) | ✅ |
| `member_added` / `member_removed` notifications | ✅ |
| `toOthers()` / `socket_id` exclusion | ✅ |
| Cache channels | ✅ every instance remembers the last event |
| Presence member list sent on subscribe (`here()`) | ❌ members connected to the same instance only |
| Channel and user counts in the HTTP API, dashboard, webhooks | ❌ per instance |
| Connection limits | ❌ per instance |

If your app relies on exact presence member lists, run a single instance (it scales far) until a
shared presence store is added (see the roadmap in the README).

### Load balancer

No sticky sessions are needed. Use a balancer that supports WebSockets and long-lived connections:

```nginx
upstream wsbro {
    least_conn;
    server 10.0.0.11:6001;
    server 10.0.0.12:6001;
}
```

On shutdown (SIGTERM) an instance closes its connections with code 1001; clients reconnect and the
balancer moves them to the remaining instances.
