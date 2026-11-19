package web

import (
	"crypto/sha256"
	"net"
	"sync"
	"time"
)

const loginWindow = 5 * time.Minute
const maxLoginKeys = 10000

type loginWindowEntry struct {
	count int
	until time.Time
}

type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]loginWindowEntry
	cleanup time.Time
}

// allow учитывает попытку входа по IP и логину и возвращает время до следующей попытки.
func (l *loginLimiter) allow(now time.Time, remoteAddr, username string) time.Duration {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	digest := sha256.Sum256([]byte(username))
	keys := [2]string{"ip:" + host, "login:" + string(digest[:])}
	limits := [2]int{30, 8}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = make(map[string]loginWindowEntry)
	}
	if !now.Before(l.cleanup) {
		for key, entry := range l.entries {
			if !now.Before(entry.until) {
				delete(l.entries, key)
			}
		}
		l.cleanup = now.Add(loginWindow)
	}

	var wait time.Duration
	missing := 0
	for i, key := range keys {
		entry, exists := l.entries[key]
		if !exists {
			missing++
		}
		if now.Before(entry.until) && entry.count >= limits[i] {
			wait = max(wait, entry.until.Sub(now))
		}
	}
	if wait > 0 {
		return wait
	}
	if missing > 0 && len(l.entries)+missing > maxLoginKeys {
		return l.cleanup.Sub(now)
	}
	for _, key := range keys {
		entry := l.entries[key]
		if !now.Before(entry.until) {
			entry = loginWindowEntry{until: now.Add(loginWindow)}
		}
		entry.count++
		l.entries[key] = entry
	}
	return 0
}
