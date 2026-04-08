package cache

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

// Valkey wraps the valkey-go client for session and API key caching.
type Valkey struct {
	client valkey.Client
}

// New creates a new Valkey cache client. Returns nil if URL is empty (cache disabled).
// Supports valkey://, redis://, and rediss:// (TLS) URLs with optional user:password auth.
func New(ctx context.Context, rawURL string) (*Valkey, error) {
	if rawURL == "" {
		return nil, nil
	}

	opts := parseValkeyURL(rawURL)
	client, err := valkey.NewClient(opts)
	if err != nil {
		return nil, fmt.Errorf("connecting to valkey: %w", err)
	}

	// Ping to verify connection
	if err := client.Do(ctx, client.B().Ping().Build()).Error(); err != nil {
		client.Close()
		return nil, fmt.Errorf("pinging valkey: %w", err)
	}

	return &Valkey{client: client}, nil
}

// Close closes the Valkey client.
func (v *Valkey) Close() {
	v.client.Close()
}

// --- Session Cache ---

const sessionPrefix = "session:"

// GetSession retrieves a cached session by ID. Returns nil if not cached.
func (v *Valkey) GetSession(ctx context.Context, sessionID string) (*auth.Session, error) {
	data, err := v.client.Do(ctx, v.client.B().Get().Key(sessionPrefix+sessionID).Build()).AsBytes()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, nil // cache miss
		}
		return nil, err
	}

	var s auth.Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, nil // corrupt cache entry, treat as miss
	}
	return &s, nil
}

// SetSession caches a session with TTL matching its expiry.
func (v *Valkey) SetSession(ctx context.Context, session *auth.Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}

	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return nil // already expired, don't cache
	}

	return v.client.Do(ctx, v.client.B().Set().Key(sessionPrefix+session.ID).Value(string(data)).Ex(ttl).Build()).Error()
}

// DeleteSession removes a session from cache.
func (v *Valkey) DeleteSession(ctx context.Context, sessionID string) error {
	return v.client.Do(ctx, v.client.B().Del().Key(sessionPrefix+sessionID).Build()).Error()
}

// --- API Key Cache ---

const keyPrefix = "apikey:"

// CachedKeyInfo is the minimal info cached for API key lookups.
type CachedKeyInfo struct {
	Role      string     `json:"role"`
	CreatedBy string     `json:"created_by"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// GetAPIKey retrieves cached API key info by hashed key. Returns nil on miss.
func (v *Valkey) GetAPIKey(ctx context.Context, hashedKey string) (*CachedKeyInfo, error) {
	data, err := v.client.Do(ctx, v.client.B().Get().Key(keyPrefix+hashedKey).Build()).AsBytes()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, nil
		}
		return nil, err
	}

	var info CachedKeyInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, nil
	}
	return &info, nil
}

// SetAPIKey caches API key info. TTL: 5 minutes (short, keys change rarely).
func (v *Valkey) SetAPIKey(ctx context.Context, hashedKey string, info CachedKeyInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return v.client.Do(ctx, v.client.B().Set().Key(keyPrefix+hashedKey).Value(string(data)).Ex(5*time.Minute).Build()).Error()
}

// DeleteAPIKey removes an API key from cache.
func (v *Valkey) DeleteAPIKey(ctx context.Context, hashedKey string) error {
	return v.client.Do(ctx, v.client.B().Del().Key(keyPrefix+hashedKey).Build()).Error()
}

// --- Helpers ---

// parseValkeyURL parses a valkey/redis URL into client options.
// Supports: valkey://host:port, redis://user:pass@host:port, rediss://host:port (TLS).
func parseValkeyURL(rawURL string) valkey.ClientOption {
	opts := valkey.ClientOption{}

	useTLS := strings.HasPrefix(rawURL, "rediss://")

	// Try standard URL parsing
	u, err := url.Parse(rawURL)
	if err != nil {
		// Fallback: treat as host:port
		opts.InitAddress = []string{rawURL}
		return opts
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "6379"
	}
	opts.InitAddress = []string{host + ":" + port}

	// Auth
	if u.User != nil {
		opts.Username = u.User.Username()
		opts.Password, _ = u.User.Password()
	}

	// TLS for rediss:// scheme
	if useTLS {
		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	return opts
}
