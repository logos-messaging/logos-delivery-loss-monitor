package ratelimit

import "testing"

func TestBucketRefillsAndReportsWait(t *testing.T) {
	b := NewBucket(10, 20)
	if ok, _ := b.Take(0, 20); !ok {
		t.Fatal("burst should be available")
	}
	ok, wait := b.Take(0, 5)
	if ok || wait != 500 {
		t.Fatalf("ok=%v wait=%d, want false/500", ok, wait)
	}
	if ok, _ := b.Take(500, 5); !ok {
		t.Fatal("expected refill after the advised wait")
	}
	if ok, _ := b.Take(10_000_000, 20); !ok {
		t.Fatal("refill is capped at burst, not unbounded")
	}
	if ok, _ := b.Take(10_000_000, 1); ok {
		t.Fatal("bucket should be empty after taking the full burst")
	}
}

func TestBucketOversizedRequestIsCappedAtBurst(t *testing.T) {
	b := NewBucket(1, 5)
	if ok, _ := b.Take(0, 50); !ok {
		t.Fatal("a request above burst is treated as burst-sized")
	}
}

func TestKeyedIsolatesKeys(t *testing.T) {
	k := NewKeyed(1, 1)
	if ok, _ := k.Take("a", 0, 1); !ok {
		t.Fatal()
	}
	if ok, _ := k.Take("a", 0, 1); ok {
		t.Fatal("a should be empty")
	}
	if ok, _ := k.Take("b", 0, 1); !ok {
		t.Fatal("b has its own bucket")
	}
}
