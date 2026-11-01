# Deployment

## Docker

```bash
docker run -d --name wsbro --restart unless-stopped \
  -p 6001:6001 \
  --ulimit nofile=1048576:1048576 \
  -e WSBRO_APP_ID=my-app \
  -e WSBRO_APP_KEY=my-key \
  -e WSBRO_APP_SECRET=my-secret \
  -e WSBRO_DASHBOARD_PASSWORD=change-me \
  -e WSBRO_LOG_FORMAT=json \
  ghcr.io/axvilab/wsbro:latest
```

The image is about 20 MB, runs as an unprivileged user, has a built-in health check and supports
`linux/amd64` and `linux/arm64`. Pin a version (`ghcr.io/axvilab/wsbro:0.2.0`) in production.

### Docker Compose next to Laravel

```yaml
services:
  wsbro:
    image: ghcr.io/axvilab/wsbro:latest
    restart: unless-stopped
    ports:
      - "6001:6001"
    ulimits:
      nofile: { soft: 1048576, hard: 1048576 }
    environment:
      WSBRO_APP_ID: ${REVERB_APP_ID}
      WSBRO_APP_KEY: ${REVERB_APP_KEY}
      WSBRO_APP_SECRET: ${REVERB_APP_SECRET}
      WSBRO_DASHBOARD_PASSWORD: ${WSBRO_DASHBOARD_PASSWORD}
      WSBRO_ALLOWED_ORIGINS: example.com
```

In Laravel's `.env`, `REVERB_HOST=wsbro` (the service name) and `VITE_REVERB_HOST` the public host.

## Binary

Download a release for your platform from GitHub Releases, or build it:

```bash
go install github.com/axvilab/wsbro@latest
# or
git clone https://github.com/axvilab/wsbro && cd wsbro && make build
```

`wsbro -version` prints the version.

### systemd

`/etc/systemd/system/wsbro.service`:

```ini
[Unit]
Description=WSBro WebSocket server
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/wsbro
EnvironmentFile=/etc/wsbro.env
User=wsbro
Restart=always
RestartSec=2
LimitNOFILE=1048576
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true

[Install]
WantedBy=multi-user.target
```

`/etc/wsbro.env` (readable by root only, it holds secrets):

```bash
WSBRO_APP_ID=my-app
WSBRO_APP_KEY=my-key
WSBRO_APP_SECRET=my-secret
WSBRO_DASHBOARD_PASSWORD=change-me
WSBRO_ALLOWED_ORIGINS=example.com
```

```bash
sudo useradd --system --no-create-home wsbro
sudo systemctl daemon-reload && sudo systemctl enable --now wsbro
journalctl -u wsbro -f
```

## TLS with a reverse proxy

Browsers on an HTTPS site need `wss://`. Let a proxy terminate TLS.

**Caddy** (gets certificates automatically):

```
ws.example.com {
    reverse_proxy 127.0.0.1:6001
}
```

**Nginx:**

```nginx
server {
    listen 443 ssl;
    http2 off;
    server_name ws.example.com;

    ssl_certificate     /etc/letsencrypt/live/ws.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/ws.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:6001;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 1h;
        proxy_buffering off;   # keeps the dashboard's live console streaming
    }
}
```

Then in Laravel: `VITE_REVERB_HOST=ws.example.com`, `VITE_REVERB_PORT=443`, `VITE_REVERB_SCHEME=https`.
Laravel itself can keep talking to WSBro over the private network (`REVERB_HOST=127.0.0.1`, `REVERB_PORT=6001`,
`REVERB_SCHEME=http`).

To serve TLS without a proxy, set `WSBRO_TLS_CERT` and `WSBRO_TLS_KEY`.

## Health checks and monitoring

- `GET /health` (or `/up`) returns `200 {"status":"ok"}`.
- `GET /metrics/prometheus` for Prometheus:

  ```yaml
  scrape_configs:
    - job_name: wsbro
      static_configs:
        - targets: ["wsbro:6001"]
      metrics_path: /metrics/prometheus
  ```

- The [dashboard](dashboard.md) at `/dashboard` for humans.

## Production checklist

- [ ] Real secrets: WSBro warns at startup when the default secret is used.
- [ ] `WSBRO_ALLOWED_ORIGINS` set to your sites.
- [ ] TLS in front (browsers on HTTPS pages need `wss://`).
- [ ] `WSBRO_MAX_CONNECTIONS` set, file descriptor limit raised.
- [ ] `WSBRO_DASHBOARD_PASSWORD` set if you want the dashboard; it is off otherwise.
- [ ] `WSBRO_LOG_FORMAT=json` if logs go to a collector.
- [ ] A pinned image tag or release version.
