// Package ratelimit provides a token bucket driven by caller-supplied time.
package ratelimit

import (
	"math"
	"sync"
)

// Bucket is a token bucket with a fixed refill rate and burst size. It is not safe for concurrent use.
type Bucket struct {
	rate   float64
	burst  float64
	tokens float64
	last   int64
	primed bool
}

// NewBucket returns a bucket that refills ratePerSec tokens per second up to burst.
func NewBucket(ratePerSec, burst float64) *Bucket {
	return &Bucket{rate: ratePerSec, burst: burst}
}

// Take removes n tokens at time nowMs. When the bucket cannot cover n it takes nothing and
// returns the wait in milliseconds until it could.
func (b *Bucket) Take(nowMs int64, n float64) (ok bool, retryAfterMs int64) {
	if !b.primed {
		b.primed, b.tokens, b.last = true, b.burst, nowMs
	}
	if nowMs > b.last {
		b.tokens = math.Min(b.burst, b.tokens+float64(nowMs-b.last)*b.rate/1000)
		b.last = nowMs
	}
	n = math.Min(n, b.burst)
	if b.tokens >= n {
		b.tokens -= n
		return true, 0
	}
	wait := int64(math.Ceil((n - b.tokens) / b.rate * 1000))
	if wait < 1 {
		wait = 1
	}
	return false, wait
}

// Keyed holds one Bucket per key behind a mutex.
type Keyed struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	buckets map[string]*Bucket
}

// NewKeyed returns a Keyed limiter whose buckets share rate and burst.
func NewKeyed(ratePerSec, burst float64) *Keyed {
	return &Keyed{rate: ratePerSec, burst: burst, buckets: map[string]*Bucket{}}
}

// Take takes n tokens from the bucket for key.
func (k *Keyed) Take(key string, nowMs int64, n float64) (bool, int64) {
	k.mu.Lock()
	defer k.mu.Unlock()
	b := k.buckets[key]
	if b == nil {
		b = NewBucket(k.rate, k.burst)
		k.buckets[key] = b
	}
	return b.Take(nowMs, n)
}
