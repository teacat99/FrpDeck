// Package captcha implements a tiny in-memory math challenge used by the
// login flow. The challenge is text-only ("12 + 7"), so it works on every
// device, never reaches a third-party (no Turnstile / reCAPTCHA) and
// keeps the embedded binary unchanged in size.
//
// Threat model: FrpDeck already has persistent IP / user lockouts and
// exponential backoff. Captcha is the *additional* hurdle that converts
// scripted attackers into "must solve a problem per request", lifting
// the per-attempt cost from "send a POST" to "OCR a string and reply",
// even though our challenge is plaintext rather than a noisy image -
// the win comes from the per-request statefulness, not visual
// distortion. A captcha is required only when the (username, ip) pair
// has crossed CaptchaThreshold failures inside the rolling window.
package captcha

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	mrand "math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teacat99/FrpDeck/internal/runtime"
)

// challengeTTL is how long a freshly-issued challenge remains valid.
// Two minutes is generous enough for slow connections while keeping
// the in-memory map bounded.
const challengeTTL = 2 * time.Minute

// FailureCounter is the slice of UserRepo the captcha service needs to
// decide when to require a challenge. Defining the interface here keeps
// captcha free of any dependency on the storage layer.
type FailureCounter interface {
	CountLoginFailuresByIP(ip string, since time.Time) (int64, error)
	CountLoginFailuresByUsername(username string, since time.Time) (int64, error)
}

// Service is goroutine-safe: every method takes its own mutex.
type Service struct {
	rt    *runtime.Settings
	store FailureCounter

	mu      sync.Mutex
	pending map[string]challenge
	rng     *mrand.Rand
}

type challenge struct {
	answer    string
	expiresAt time.Time
}

// New constructs a Service. rt drives the threshold; store is read in
// Required() to decide whether the caller is past it.
func New(rt *runtime.Settings, store FailureCounter) *Service {
	return &Service{
		rt:      rt,
		store:   store,
		pending: make(map[string]challenge),
		rng:     mrand.New(mrand.NewSource(time.Now().UnixNano())),
	}
}

// Required reports whether the next login attempt must include a
// captcha. We OR the per-IP and per-user counts so an attacker can't
// dodge the gate by rotating either dimension.
func (s *Service) Required(username, ip string) bool {
	thr := s.rt.CaptchaThreshold()
	if thr <= 0 {
		return false
	}
	since := time.Now().Add(-s.rt.LoginIPWindow())
	if n, err := s.store.CountLoginFailuresByIP(ip, since); err == nil && int(n) >= thr {
		return true
	}
	since = time.Now().Add(-s.rt.LoginUserWindow())
	if n, err := s.store.CountLoginFailuresByUsername(username, since); err == nil && int(n) >= thr {
		return true
	}
	return false
}

// Issue returns a fresh (id, question) pair. The answer is stored
// server-side keyed by id; the client never sees it. We sweep stale
// entries opportunistically here so the map stays bounded without
// needing a goroutine.
func (s *Service) Issue() (id, question string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = newID()
	question, answer := s.gen()
	s.pending[id] = challenge{answer: answer, expiresAt: time.Now().Add(challengeTTL)}
	s.gcLocked()
	return id, question
}

// Verify consumes the challenge: a correct answer matches once, then
// the entry is deleted whether right or wrong, so an attacker cannot
// retry the same id with different answers.
func (s *Service) Verify(id, answer string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.pending[id]
	if !ok {
		return false
	}
	delete(s.pending, id)
	if time.Now().After(c.expiresAt) {
		return false
	}
	return strings.TrimSpace(answer) == c.answer
}

// gen produces an addition or subtraction problem with single-digit
// operands. Subtraction is bounded to non-negative results so we never
// confuse users with a minus-sign edge case.
func (s *Service) gen() (question, answer string) {
	a := s.rng.Intn(20) + 1
	b := s.rng.Intn(20) + 1
	op := "+"
	if s.rng.Intn(2) == 0 {
		op = "-"
		if b > a {
			a, b = b, a
		}
	}
	switch op {
	case "+":
		return fmt.Sprintf("%d + %d", a, b), strconv.Itoa(a + b)
	default:
		return fmt.Sprintf("%d - %d", a, b), strconv.Itoa(a - b)
	}
}

func (s *Service) gcLocked() {
	now := time.Now()
	for id, c := range s.pending {
		if now.After(c.expiresAt) {
			delete(s.pending, id)
		}
	}
}

func newID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}
