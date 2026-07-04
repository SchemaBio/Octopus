package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateEntry struct {
	count       int
	windowStart time.Time
}

// RateLimiter is a small in-process fixed-window limiter suitable for public
// authentication endpoints. It is intentionally best-effort; deployments that
// run multiple replicas should still enforce edge-level rate limits.
type RateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateEntry
	maxReqs int
	window  time.Duration
}

func NewRateLimiter(maxReqs int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		entries: make(map[string]*rateEntry),
		maxReqs: maxReqs,
		window:  window,
	}
	cleanupInterval := window / 4
	if cleanupInterval < time.Second {
		cleanupInterval = time.Second
	}
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			rl.cleanup()
		}
	}()
	return rl
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for k, v := range rl.entries {
		if now.Sub(v.windowStart) > rl.window {
			delete(rl.entries, k)
		}
	}
}

func (rl *RateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	entry, exists := rl.entries[key]
	if !exists || now.Sub(entry.windowStart) > rl.window {
		rl.entries[key] = &rateEntry{count: 1, windowStart: now}
		return true
	}
	if entry.count >= rl.maxReqs {
		return false
	}
	entry.count++
	return true
}

func IPRateLimit(maxReqs int, window time.Duration) gin.HandlerFunc {
	limiter := NewRateLimiter(maxReqs, window)
	return func(c *gin.Context) {
		if !limiter.allow(c.ClientIP()) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Rate limit exceeded, please try again later"})
			c.Abort()
			return
		}
		c.Next()
	}
}
