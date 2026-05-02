// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"math/rand"
	"strconv"
	"time"
)

// RetryPolicy controls the per-call retry behaviour. The defaults match
// the plan: max 3 retries, initial 500 ms, ×2 backoff, ±25% jitter,
// capped at 8 s. 5xx and transport errors retry; 4xx never retry; 429
// honours Retry-After when present.
type RetryPolicy struct {
	MaxRetries  int
	InitialWait time.Duration
	Multiplier  float64
	JitterFrac  float64
	MaxWait     time.Duration
}

// DefaultRetryPolicy returns the locked plan defaults.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:  3,
		InitialWait: 500 * time.Millisecond,
		Multiplier:  2,
		JitterFrac:  0.25,
		MaxWait:     8 * time.Second,
	}
}

// nextWait returns the wait duration before retry attempt n (0-indexed —
// attempt 0 is the first retry, not the initial call). honourRetryAfter
// overrides the computed backoff when the server provided a Retry-After
// header value.
func (p RetryPolicy) nextWait(attempt int, honourRetryAfter string, rng *rand.Rand) time.Duration {
	if d := parseRetryAfter(honourRetryAfter); d > 0 {
		if d > p.MaxWait {
			return p.MaxWait
		}
		return d
	}
	base := float64(p.InitialWait)
	for i := 0; i < attempt; i++ {
		base *= p.Multiplier
	}
	if base > float64(p.MaxWait) {
		base = float64(p.MaxWait)
	}
	if p.JitterFrac > 0 && rng != nil {
		j := (rng.Float64()*2 - 1) * p.JitterFrac // [-JitterFrac, +JitterFrac)
		base *= 1 + j
		if base < 0 {
			base = 0
		}
	}
	return time.Duration(base)
}

// parseRetryAfter parses an HTTP Retry-After header. Only the
// delta-seconds form is supported; HTTP-date forms return 0.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}
