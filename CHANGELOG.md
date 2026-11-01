# Changelog

## 0.2.0

A rewrite focused on correctness, security and running in production.

### Fixed
- Events triggered through the HTTP API were delivered with `channel` and `data` swapped, so Laravel
  Echo never received them.
- With Redis or NATS, every event was delivered twice and `toOthers()` was ignored; NATS did not relay
  channels with dots, such as Laravel's `private-App.Models.User.1`.
- Apps were not isolated: channels were shared between apps and the app ID in the URL was not checked.
- Channel listing endpoints of the HTTP API required no authentication.
- `body_md5` was not compared with the request body, so a captured request could be replayed with a
  different payload.
- Clients could send `client-*` events to channels they were not subscribed to.
- Presence: a user with several tabs triggered `member_added`/`member_removed` for every tab.
- `channel_vacated` webhooks were sent for unrelated channels on every disconnect; data races and a
  race where a subscriber could land in a removed channel.
- A slow client could stall broadcasts to everyone; dead connections were never detected.

### Added
- Dashboard rewritten: channels with presence members and test events, connections with disconnect,
  live debug console, charts kept on the server, apps and server overview, light and dark themes.
  No CDN, works offline, disabled unless a password is set.
- Pusher features: cache channels, user authentication (`pusher:signin`, `sendToUser`),
  `terminate_connections`, `info` parameters of the channel API, Pusher error codes.
- Webhooks in the Pusher format, signed, verifiable with the official SDKs, delivered in order.
- Per-app limits, allowed origins and webhooks; TLS; JSON logs and log levels; `/health`;
  per-app Prometheus metrics; `-version`.
- Laravel 12 and 13 end-to-end tests with laravel-echo and pusher-js, `reverb` and `pusher` broadcasters.
- Load generator (`cmd/wsbro-bench`); 100,000 concurrent connections verified.

### Changed
- Webhook payloads use the Pusher format (`time_ms` + `events`). `connection_established` and
  `connection_closed` webhooks were removed: Pusher does not have them, and the debug console shows
  connections live.
- The dashboard is disabled unless `WSBRO_DASHBOARD_PASSWORD` is set.
- Invalid configuration stops the server at startup.
- The Docker image runs as an unprivileged user.

## 0.1.0

Initial release.
