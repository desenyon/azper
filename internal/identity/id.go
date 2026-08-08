package identity

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"time"
)

const randomBytes = 10

var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// New returns a stable, time-sortable identifier with an entity prefix.
func New(prefix string, now time.Time) (string, error) {
	if !validPrefix(prefix) {
		return "", fmt.Errorf("invalid identifier prefix %q", prefix)
	}

	random := make([]byte, randomBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("read random identifier bytes: %w", err)
	}

	return fmt.Sprintf(
		"%s_%013d_%s",
		prefix,
		now.UTC().UnixMilli(),
		strings.ToLower(encoding.EncodeToString(random)),
	), nil
}

func validPrefix(prefix string) bool {
	if prefix == "" {
		return false
	}
	for _, r := range prefix {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
