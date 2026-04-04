# Deployment

## Binary

Build and copy to your server:

```bash
# Build for Linux (from any OS)
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o wsbro .

# Copy to server
scp wsbro user@your-server:/opt/wsbro/

# Run
ssh user@your-server "/opt/wsbro/wsbro"
```

## Systemd Service

Create `/etc/systemd/system/wsbro.service`:

```ini
[Unit]
Description=WSBro WebSocket Server
After=network.target

[Service]
Type=simple
User=www-data
ExecStart=/opt/wsbro/wsbro
Restart=on-failure
RestartSec=5

Environment=WSBRO_PORT=6001
Environment=WSBRO_APP_ID=your-app-id
Environment=WSBRO_APP_KEY=your-app-key
Environment=WSBRO_APP_SECRET=your-app-secret
Environment=WSBRO_DASHBOARD_PASSWORD=your-password

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable wsbro
sudo systemctl start wsbro
sudo systemctl status wsbro
```

## Docker

```bash
docker run -d \
  --name wsbro \
  --restart unless-stopped \
  -p 6001:6001 \
  -e WSBRO_APP_ID=your-app-id \
  -e WSBRO_APP_KEY=your-app-key \
  -e WSBRO_APP_SECRET=your-app-secret \
  -e WSBRO_DASHBOARD_PASSWORD=your-password \
  axvi/wsbro
```

## Docker Compose

```yaml
services:
  wsbro:
    image: axvi/wsbro
    ports:
      - "6001:6001"
    environment:
      WSBRO_APP_ID: "your-app-id"
      WSBRO_APP_KEY: "your-app-key"
      WSBRO_APP_SECRET: "your-app-secret"
      WSBRO_DASHBOARD_PASSWORD: "your-password"
    restart: unless-stopped
```

## Nginx Reverse Proxy

To serve WSBro behind Nginx with SSL:

```nginx
upstream wsbro {
    server 127.0.0.1:6001;
}

server {
    listen 443 ssl;
    server_name ws.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/ws.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/ws.yourdomain.com/privkey.pem;

    location / {
        proxy_pass http://wsbro;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 86400;
    }
}
```

Then update your Laravel `.env`:

```env
PUSHER_HOST=ws.yourdomain.com
PUSHER_PORT=443
PUSHER_SCHEME=https
```

And Laravel Echo:

```javascript
window.Echo = new Echo({
    broadcaster: 'pusher',
    key: import.meta.env.VITE_PUSHER_APP_KEY,
    wsHost: 'ws.yourdomain.com',
    wsPort: 443,
    wssPort: 443,
    forceTLS: true,
    enabledTransports: ['ws', 'wss'],
    cluster: '',
});
```

## Caddy Reverse Proxy

If you use Caddy (simpler SSL setup):

```
ws.yourdomain.com {
    reverse_proxy localhost:6001
}
```

Caddy handles SSL automatically.

## With FrankenPHP

If your Laravel runs on FrankenPHP + Caddy, add WSBro as a separate upstream:

```
ws.yourdomain.com {
    reverse_proxy localhost:6001
}

yourdomain.com {
    php_server
}
```

## Health Check

Use the root endpoint for health checks:

```bash
curl http://localhost:6001/
# WSBro is running on port 6001
```

For Docker health checks:

```yaml
services:
  wsbro:
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:6001/"]
      interval: 30s
      timeout: 5s
      retries: 3
```