package auth

import (
	"context"
	"net"
)

// Limit login initiations per server-observed remote address and provider.
// Do not trust client-supplied X-Forwarded-For without a trusted reverse proxy.
func (s Store) AllowLoginStart(ctx context.Context, provider, remoteAddr string) (bool, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	if host == "" {
		host = "unknown"
	}
	key := digest("oauth-start:" + provider + ":" + host)
	var attempts int
	err = s.DB.QueryRow(ctx, `INSERT INTO auth_rate_limits AS limits (key_hash, attempts, reset_at)
		VALUES ($1, 1, now() + interval '10 minutes')
		ON CONFLICT (key_hash) DO UPDATE
		SET attempts = CASE WHEN limits.reset_at <= now() THEN 1 ELSE limits.attempts + 1 END,
		    reset_at = CASE WHEN limits.reset_at <= now()
		               THEN now() + interval '10 minutes' ELSE limits.reset_at END
		RETURNING attempts`, key).Scan(&attempts)
	if err != nil {
		return false, err
	}
	return attempts <= 20, nil
}
