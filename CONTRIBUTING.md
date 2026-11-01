# Contributing

Thanks for helping! Bug reports, ideas and pull requests are welcome.

## Development

You need Go 1.26+ and Docker (for the integration and end-to-end tests).

```bash
make run    # server on :6001, dashboard on /dashboard (password: secret)
make test   # unit tests with the race detector
make lint   # gofmt, vet, staticcheck, govulncheck
```

Before opening a pull request for protocol changes, also run:

```bash
make test-integration                     # Redis and NATS adapters against real servers
make e2e                                  # Laravel 13 + laravel-echo, reverb broadcaster
LARAVEL=12 BROADCASTER=pusher make e2e    # other combinations
```

## Project layout

```
main.go                 configuration, logging, startup and graceful shutdown
internal/config         environment variables and validation
internal/server         Pusher protocol, HTTP API, webhooks, metrics, dashboard API
internal/channel        subscribers and presence members of one channel
internal/connection     one WebSocket with its send queue
internal/adapter        Redis and NATS relays between instances
dashboard/              the embedded dashboard (Preact + htm, no build step)
cmd/wsbro-bench         load generator
examples/laravel        Laravel end-to-end test suite
```

## Guidelines

- Keep it simple: readable code beats clever code, and a new dependency needs a strong reason.
- Behaviour must match Pusher. Link the relevant part of the Pusher docs in a comment when it is not obvious.
- Every bug fix comes with a test that fails without it.
- Run `gofmt`; CI rejects unformatted code.

## Reporting security issues

Please do not open public issues for vulnerabilities, see [SECURITY.md](SECURITY.md).
