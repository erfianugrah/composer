package app

// StackTemplate is a pre-built compose configuration for common self-hosted apps.
type StackTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Compose     string `json:"compose"`
	Icon        string `json:"icon,omitempty"` // emoji or URL
}

// BuiltinTemplates returns all available stack templates.
func BuiltinTemplates() []StackTemplate {
	return []StackTemplate{
		{
			ID: "nginx", Name: "Nginx", Description: "Lightweight web server and reverse proxy",
			Category: "Web Server", Icon: "🌐",
			Compose: `services:
  nginx:
    image: nginx:alpine
    ports:
      - "8080:80"
    volumes:
      - ./html:/usr/share/nginx/html:ro
    restart: unless-stopped
`,
		},
		{
			ID: "caddy", Name: "Caddy", Description: "Automatic HTTPS web server",
			Category: "Web Server", Icon: "🔒",
			Compose: `services:
  caddy:
    image: caddy:2-alpine
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
    restart: unless-stopped

volumes:
  caddy_data:
  caddy_config:
`,
		},
		{
			ID: "postgres", Name: "PostgreSQL", Description: "Relational database",
			Category: "Database", Icon: "🐘",
			Compose: `services:
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: changeme
      POSTGRES_DB: appdb
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U app"]
      interval: 5s
      timeout: 3s
      retries: 5
    restart: unless-stopped

volumes:
  pgdata:
`,
		},
		{
			ID: "valkey", Name: "Valkey", Description: "In-memory data store (Redis fork)",
			Category: "Database", Icon: "⚡",
			Compose: `services:
  valkey:
    image: valkey/valkey:8-alpine
    ports:
      - "6379:6379"
    volumes:
      - valkey_data:/data
    restart: unless-stopped

volumes:
  valkey_data:
`,
		},
		{
			ID: "uptime-kuma", Name: "Uptime Kuma", Description: "Self-hosted monitoring tool",
			Category: "Monitoring", Icon: "📊",
			Compose: `services:
  uptime-kuma:
    image: louislam/uptime-kuma:latest
    ports:
      - "3001:3001"
    volumes:
      - uptime-kuma-data:/app/data
    restart: unless-stopped

volumes:
  uptime-kuma-data:
`,
		},
		{
			ID: "vaultwarden", Name: "Vaultwarden", Description: "Bitwarden-compatible password manager",
			Category: "Security", Icon: "🔑",
			Compose: `services:
  vaultwarden:
    image: vaultwarden/server:latest
    environment:
      SIGNUPS_ALLOWED: "false"
    ports:
      - "8080:80"
    volumes:
      - vw-data:/data
    restart: unless-stopped

volumes:
  vw-data:
`,
		},
		{
			ID: "gitea", Name: "Gitea", Description: "Lightweight self-hosted Git service",
			Category: "Developer", Icon: "🍵",
			Compose: `services:
  gitea:
    image: gitea/gitea:latest
    environment:
      USER_UID: "1000"
      USER_GID: "1000"
    ports:
      - "3000:3000"
      - "2222:22"
    volumes:
      - gitea-data:/data
    restart: unless-stopped

volumes:
  gitea-data:
`,
		},
		{
			ID: "portainer-agent", Name: "Portainer Agent", Description: "Remote Docker management agent",
			Category: "Management", Icon: "🐳",
			Compose: `services:
  portainer-agent:
    image: portainer/agent:latest
    ports:
      - "9001:9001"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /var/lib/docker/volumes:/var/lib/docker/volumes
    restart: unless-stopped
`,
		},
		{
			ID: "whoami", Name: "Whoami", Description: "Simple HTTP service for testing",
			Category: "Developer", Icon: "🧪",
			Compose: `services:
  whoami:
    image: traefik/whoami:latest
    ports:
      - "8080:80"
    restart: unless-stopped
`,
		},
		{
			ID: "immich", Name: "Immich", Description: "Self-hosted photo and video management",
			Category: "Media", Icon: "📸",
			Compose: `# Immich requires additional setup. See https://immich.app/docs/install/docker-compose
# This is a simplified template -- refer to official docs for full configuration.
services:
  immich-server:
    image: ghcr.io/immich-app/immich-server:release
    ports:
      - "2283:2283"
    volumes:
      - upload:/usr/src/app/upload
    environment:
      DB_HOSTNAME: immich-db
      DB_USERNAME: postgres
      DB_PASSWORD: changeme
      DB_DATABASE_NAME: immich
      REDIS_HOSTNAME: immich-redis
    depends_on:
      - immich-db
      - immich-redis
    restart: unless-stopped

  immich-db:
    image: tensorchord/pgvecto-rs:pg17-v0.4.0
    environment:
      POSTGRES_PASSWORD: changeme
      POSTGRES_USER: postgres
      POSTGRES_DB: immich
    volumes:
      - pgdata:/var/lib/postgresql/data
    restart: unless-stopped

  immich-redis:
    image: valkey/valkey:8-alpine
    restart: unless-stopped

volumes:
  upload:
  pgdata:
`,
		},
	}
}

// GetTemplate returns a template by ID, or nil if not found.
func GetTemplate(id string) *StackTemplate {
	for _, t := range BuiltinTemplates() {
		if t.ID == id {
			return &t
		}
	}
	return nil
}

// ListTemplatesByCategory returns templates grouped by category.
func ListTemplatesByCategory() map[string][]StackTemplate {
	groups := make(map[string][]StackTemplate)
	for _, t := range BuiltinTemplates() {
		groups[t.Category] = append(groups[t.Category], t)
	}
	return groups
}
