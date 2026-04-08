package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

// Valkey wraps the valkey-go client for session and API key caching.
type Valkey struct {
	client valkey.Client
}

// New creates a new Valkey cache client. Returns nil if URL is empty (cache disabled).
func New(ctx context.Context, url string) (*Valkey, error) {
	if url == "" {
		return nil, nil
	}

	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{parseValkeyAddr(url)},
	})
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

func parseValkeyAddr(url string) string {
	// Strip valkey:// or redis:// prefix
	addr := url
	for _, prefix := range []string{"valkey://", "redis://", "rediss://"} {
		if len(addr) > len(prefix) && addr[:len(prefix)] == prefix {
			addr = addr[len(prefix):]
			break
		}
	}
	// Default port
	if !contains(addr, ":") {
		addr += ":6379"
	}
	return addr
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
