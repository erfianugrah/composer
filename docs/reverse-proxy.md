# Reverse Proxy

Composer serves HTTP on port 8080. For production, put a reverse proxy in front for TLS, domain routing, and security headers.

## Caddy (recommended)

Caddy handles TLS automatically via Let's Encrypt. No certificate management needed.

### Caddyfile

```caddyfile
composer.example.com {
    reverse_proxy localhost:8080

    # WebSocket terminal support (Caddy handles this automatically)
    # SSE support (Caddy handles this automatically -- no buffering by default)
}
```

That's it. Caddy auto-provisions TLS certificates.

### With the Composer stack behind Caddy

If Caddy and Composer are both Docker containers on the same network:

```caddyfile
composer.example.com {
    reverse_proxy composer:8080
}
```

### Caddyfile with security headers

```caddyfile
composer.example.com {
    reverse_proxy composer:8080

    header {
        X-Content-Type-Options "nosniff"
        X-Frame-Options "DENY"
        Referrer-Policy "strict-origin-when-cross-origin"
        Permissions-Policy "camera=(), microphone=(), geolocation=()"
        -Server
    }
}
```

### Docker Compose with Caddy

If you want Caddy in the same compose file as Composer:

```yaml
services:
  caddy:
    image: caddy:2-alpine
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp"  # HTTP/3
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
    restart: unless-stopped

  composer:
    image: ghcr.io/erfianugrah/composer:latest
    # No port mapping needed -- Caddy proxies internally
    expose:
      - "8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - stacks:/opt/stacks
    environment:
      PUID: "1000"
      PGID: "1000"
      COMPOSER_DB_URL: "postgres://composer:secret@postgres:5432/composer?sslmode=disable"
    depends_on:
      postgres:
        condition: service_healthy
    restart: unless-stopped
    read_only: true
    tmpfs:
      - /tmp
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    cap_add:
      - CHOWN
      - SETUID
      - SETGID
      - DAC_OVERRIDE

  postgres:
    image: postgres:17-alpine
    volumes:
      - pgdata:/var/lib/postgresql/data
    environment:
      POSTGRES_USER: composer
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: composer
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U composer"]
      interval: 5s
      timeout: 3s
      retries: 5
    restart: unless-stopped

volumes:
  caddy_data:
  caddy_config:
  stacks:
  pgdata:
```

With a `Caddyfile`:

```caddyfile
composer.example.com {
    reverse_proxy composer:8080
}
```

### Behind an existing Caddy (your setup)

If you already run Caddy as a reverse proxy (like on your server), just add the Composer upstream to your existing Caddyfile:

```caddyfile
composer.example.com {
    reverse_proxy composer:8080
}
```

No port mapping needed on the Composer container -- just `expose: ["8080"]` and put it on the same Docker network as Caddy.

### SSE and WebSocket notes for Caddy

Caddy handles both SSE and WebSocket transparently:
- **SSE**: Caddy does NOT buffer SSE responses by default (unlike nginx). Events stream in real-time with no extra config.
- **WebSocket**: Caddy auto-detects the `Upgrade: websocket` header and proxies bidirectionally. No special config needed.

## Traefik

If you use Traefik (common in Docker/Kubernetes setups):

### Docker labels

```yaml
services:
  composer:
    image: ghcr.io/erfianugrah/composer:latest
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.composer.rule=Host(`composer.example.com`)"
      - "traefik.http.routers.composer.entrypoints=websecure"
      - "traefik.http.routers.composer.tls.certresolver=letsencrypt"
      - "traefik.http.services.composer.loadbalancer.server.port=8080"
      # SSE: disable response buffering
      - "traefik.http.middlewares.composer-sse.buffering.maxResponseBodyBytes=0"
      - "traefik.http.routers.composer.middlewares=composer-sse"
```

Traefik handles WebSocket automatically but needs buffering disabled for SSE.

## nginx

If you use nginx:

```nginx
server {
    listen 443 ssl http2;
    server_name composer.example.com;

    ssl_certificate     /etc/ssl/certs/composer.pem;
    ssl_certificate_key /etc/ssl/private/composer.key;

    location / {
        proxy_pass http://composer:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";

        # SSE support -- disable buffering
        proxy_buffering off;
        proxy_cache off;

        # Long-lived connections for SSE/WS
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }
}
```

nginx requires explicit configuration for both WebSocket (`Upgrade` headers) and SSE (`proxy_buffering off`). This is the most common source of issues -- if SSE events are delayed or batched, check that buffering is disabled.

## Session Cookie and TLS

Composer's session cookie is set with `Secure: false` by default (so it works over plain HTTP during development). When behind a TLS-terminating reverse proxy, the cookie still works because the browser sees HTTPS.

For strict environments, set `COMPOSER_COOKIE_SECURE=true` to force the `Secure` flag on the session cookie.
