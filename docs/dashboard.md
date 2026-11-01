# Dashboard

A built-in admin panel at `http://your-server:6001/dashboard`. It is part of the binary: no Node.js,
no CDN, works offline.

Enable it with a password (any user name is accepted):

```bash
WSBRO_DASHBOARD_PASSWORD=change-me
```

Without a password the dashboard is **disabled**, because it shows channel names, user data and event
payloads.

## Pages

**Overview**: connections, channels, messages and API events per second, memory and uptime; charts
for the last 5 minutes, 15 minutes or hour (kept on the server, so they survive page reloads); usage
per app against its connection limit.

**Channels**: every occupied channel with search, type filter and pagination (done on the server, so
it stays fast on a busy server). Click a channel to see its presence members with their `user_info`, its live
activity, and to **send a test event** to it.

**Connections**: open connections with socket ID, signed-in user, browser, IP and age; search by any of
them and **disconnect** a client (it reconnects right away).

**Debug console**: a live stream of what happens in the app: connections, subscriptions, failed
authorizations, presence joins and leaves, API events and whispers with their payloads. Filter by kind
and text, pause, expand any line to see the payload. The first place to look when "the event does not
arrive".

**Apps & server**: configured apps and their limits (secrets are never shown, webhook URLs are
redacted), server settings, and a ready-to-copy Laravel `.env` block for the selected app.

## Security

- HTTP Basic Auth, compared in constant time.
- Actions (send event, disconnect) also require a custom request header, so other sites cannot trigger
  them through the browser (CSRF).
- Strict Content Security Policy: the page loads nothing from other origins.
- The live console costs nothing while no dashboard is open.

Serve it over HTTPS (see [deployment](deployment.md)), since Basic Auth sends the password with every
request.
