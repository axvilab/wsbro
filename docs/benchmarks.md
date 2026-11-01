# Benchmarks

Measured on one machine (16 CPU cores, 30 GB RAM, Linux), WSBro and the load generator in separate
Docker containers. The load generator is in this repository, so you can repeat everything:

```bash
go run ./cmd/wsbro-bench -host localhost:6001 -conns 10000 -channels 100 -events 20
```

It opens the connections, subscribes them evenly to the channels, triggers events through the signed
HTTP API and measures the time from the API call to the moment each client receives the event.

## Results

| Scenario | Delivered | Throughput | Latency p50 / p95 / p99 |
|---|---|---|---|
| 10,000 clients, 100 channels, 2,000 events | 200,000 / 200,000 | 160,000 msg/s | 0.8 / 1.7 / 3.1 ms |
| 20,000 clients, 2 instances over **Redis** | 200,000 / 200,000 | 142,000 msg/s | 1.2 / 2.2 / 3.4 ms |
| 20,000 clients, 2 instances over **NATS** | 200,000 / 200,000 | 150,000 msg/s | 1.7 / 5.8 / 30 ms |
| **100,000** concurrent clients, one instance, events to 500 channels | all | — | 0.8 / 4.6 / 9.5 ms * |

\* measured while the load generator itself was holding the 100,000 sockets on the same machine.

## Memory

| Connections | Server memory |
|---|---|
| 0 | 3 MB |
| 50,000 | 1.6 GB |
| 100,000 | 3.3 GB |

About 33 KB per connection, including Go runtime overhead: the socket buffers, one goroutine for
reading, one for writing and a 64-message send queue.

## How one slow client is handled

Every connection has its own send queue. Broadcasting never waits for a client: a client that cannot
keep up (a phone on a bad network) fills its queue and is disconnected with code 4100, so it reconnects
and catches up, while everyone else keeps receiving events in real time.
