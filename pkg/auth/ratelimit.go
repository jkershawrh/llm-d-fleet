package auth

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultMaxBuckets caps how many distinct keys a limiter will track.
//
// The map is keyed on remote identity, so its size is bounded by the number
// of real clients under normal operation. The cap exists for the abnormal
// case: a distributed flood from many source addresses would otherwise grow
// the map without limit, turning the component that prevents resource
// exhaustion into the cause of it.
const DefaultMaxBuckets = 50_000

// RateLimiter implements a per-key token bucket rate limiter.
type RateLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*tokenBucket
	rate       float64 // tokens per second
	burst      int     // max bucket size
	ttl        time.Duration
	maxBuckets int
	done       chan struct{}

	// trustProxyHeaders allows X-Forwarded-For to determine the client
	// address. Only enable it when every request reaches this process
	// through a proxy that overwrites the header; otherwise any client can
	// forge its own rate-limit identity.
	trustProxyHeaders bool
}

type tokenBucket struct {
	tokens     float64
	lastCheck  time.Time
	lastAccess time.Time
}

// NewRateLimiterWithTTL creates a new per-key rate limiter with a custom TTL
// for bucket eviction. Buckets not accessed within the TTL are automatically
// removed by a background sweep goroutine. Call Stop() to release the goroutine.
func NewRateLimiterWithTTL(ratePerSecond float64, burstSize int, ttl time.Duration) *RateLimiter {
	rl := &RateLimiter{
		buckets:    make(map[string]*tokenBucket),
		rate:       ratePerSecond,
		burst:      burstSize,
		ttl:        ttl,
		maxBuckets: DefaultMaxBuckets,
		done:       make(chan struct{}),
	}
	go rl.sweepLoop()
	return rl
}

func (rl *RateLimiter) sweepLoop() {
	ticker := time.NewTicker(rl.ttl / 2)
	defer ticker.Stop()
	for {
		select {
		case <-rl.done:
			return
		case <-ticker.C:
			rl.mu.Lock()
			rl.evictExpiredLocked(time.Now())
			rl.mu.Unlock()
		}
	}
}

// evictExpiredLocked removes buckets untouched for longer than the TTL.
// The caller must hold rl.mu.
func (rl *RateLimiter) evictExpiredLocked(now time.Time) {
	for key, b := range rl.buckets {
		if now.Sub(b.lastAccess) > rl.ttl {
			delete(rl.buckets, key)
		}
	}
}

// TrustProxyHeaders controls whether X-Forwarded-For is honoured when
// determining a client address. Leave it off unless this process sits behind
// a proxy that always overwrites the header — when it is on and the process
// is reachable directly, any client can choose its own rate-limit identity.
func (rl *RateLimiter) TrustProxyHeaders(trust bool) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.trustProxyHeaders = trust
}

// Derive returns a second limiter with the same rate, burst, TTL and proxy
// trust setting, but its own bucket map. Use it to limit on a different key
// space (for example authenticated subject rather than client address).
// The returned limiter owns a sweep goroutine and must be Stopped.
func (rl *RateLimiter) Derive() *RateLimiter {
	rl.mu.Lock()
	rate, burst, ttl, trust := rl.rate, rl.burst, rl.ttl, rl.trustProxyHeaders
	rl.mu.Unlock()

	derived := NewRateLimiterWithTTL(rate, burst, ttl)
	derived.TrustProxyHeaders(trust)
	return derived
}

// Stop stops the background eviction goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.done)
}

// BucketCount returns the number of active token buckets.
func (rl *RateLimiter) BucketCount() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return len(rl.buckets)
}

// NewRateLimiter creates a new per-key rate limiter. ratePerSecond is the
// sustained rate (tokens refilled per second) and burstSize is the maximum
// number of tokens a bucket can hold (i.e. the burst capacity).
func NewRateLimiter(ratePerSecond float64, burstSize int) *RateLimiter {
	return NewRateLimiterWithTTL(ratePerSecond, burstSize, 10*time.Minute)
}

// Allow checks if a request from the given key is allowed. It consumes one
// token from the key's bucket and returns true if the token was available.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		if rl.maxBuckets > 0 && len(rl.buckets) >= rl.maxBuckets {
			// Reclaim what the periodic sweep has not yet collected.
			rl.evictExpiredLocked(now)
		}
		if rl.maxBuckets > 0 && len(rl.buckets) >= rl.maxBuckets {
			// Still saturated: refuse rather than grow without bound. Every
			// tracked key is an active client, so this is a real flood and
			// 429 is the correct answer.
			return false
		}
		// First request for this key: start with a full bucket minus one token.
		rl.buckets[key] = &tokenBucket{
			tokens:     float64(rl.burst) - 1,
			lastCheck:  now,
			lastAccess: now,
		}
		return true
	}

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(b.lastCheck).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastCheck = now
	b.lastAccess = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// clientAddrKey derives a rate-limit key from the client's network address.
//
// The key must not be selectable by the caller. Anything a client can vary
// freely — a tenant header, an unvalidated forwarding header — lets it mint a
// fresh bucket per request and bypass the limit entirely, so only the peer
// address is trusted by default.
//
// X-Forwarded-For is honoured only when the limiter is explicitly configured
// to trust it, which is correct behind a proxy that overwrites the header and
// unsafe anywhere else.
func clientAddrKey(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// The left-most entry is the originating client.
			if idx := strings.IndexByte(xff, ','); idx != -1 {
				return "ip:" + strings.TrimSpace(xff[:idx])
			}
			return "ip:" + strings.TrimSpace(xff)
		}
	}
	addr := r.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	return "ip:" + addr
}

// subjectKey derives a rate-limit key from verified identity, so each
// authenticated principal gets its own budget regardless of source address.
// It falls back to the client address when no claims are present.
func subjectKey(r *http.Request, trustProxyHeaders bool) string {
	if identity := GetIdentity(r); identity != nil && identity.Subject != "" {
		return "sub:" + identity.Subject
	}
	// Preserve compatibility for internal callers that still install legacy
	// claims directly rather than passing through AuthMiddleware.
	if claims := GetClaims(r); claims != nil && claims.Subject != "" {
		return "sub:" + claims.Subject
	}
	return clientAddrKey(r, trustProxyHeaders)
}

// RateLimitMiddleware wraps an http.Handler with client-address rate limiting.
// Returns 429 Too Many Requests with a JSON body when the rate is exceeded.
func RateLimitMiddleware(limiter *RateLimiter, next http.Handler) http.Handler {
	return RateLimitMiddlewareWithExemptions(limiter, nil, next)
}

// RateLimitMiddlewareWithExemptions limits by client address, bypassing
// exact-match exempt paths such as liveness and readiness probes.
//
// This tier runs ahead of authentication, so it is the only thing standing
// between an unauthenticated client and the token verification path. Key it
// on the peer address and nothing the caller controls.
func RateLimitMiddlewareWithExemptions(limiter *RateLimiter, exempt []string, next http.Handler) http.Handler {
	return rateLimitMiddleware(limiter, exempt, clientAddrKey, next)
}

// SubjectRateLimitMiddleware limits by authenticated subject, giving each
// principal its own budget. It must be mounted inside AuthMiddleware so that
// verified claims are on the request context; without claims it falls back to
// the client address.
func SubjectRateLimitMiddleware(limiter *RateLimiter, exempt []string, next http.Handler) http.Handler {
	return rateLimitMiddleware(limiter, exempt, subjectKey, next)
}

func rateLimitMiddleware(limiter *RateLimiter, exempt []string, keyFn func(*http.Request, bool) string, next http.Handler) http.Handler {
	exemptSet := make(map[string]bool, len(exempt))
	for _, p := range exempt {
		exemptSet[p] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exemptSet[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		limiter.mu.Lock()
		trust := limiter.trustProxyHeaders
		limiter.mu.Unlock()

		if !limiter.Allow(keyFn(r, trust)) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":  "rate limit exceeded",
				"detail": "too many requests, please retry later",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
