package middleware

import (
	"io"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/httpx"
)

// Chaos lets the presenter make the API fail on demand so the room can watch
// retries. It is installed only when DEMO_CONTROL is set, and configured
// through the control feature.

// ChaosMode is what a failed request looks like to the client.
type ChaosMode string

// Modes.
const (
	ChaosNone    ChaosMode = "none"
	Chaos500     ChaosMode = "500"
	ChaosTimeout ChaosMode = "timeout"
	ChaosDrop    ChaosMode = "drop"
)

// ChaosState is the current setting, guarded by a mutex because the control
// handler writes it while request handlers read it.
type ChaosState struct {
	mu        sync.RWMutex
	FailRate  float64
	LatencyMs int
	Mode      ChaosMode
}

// NewChaos returns an inert state.
func NewChaos() *ChaosState { return &ChaosState{Mode: ChaosNone} }

// Set replaces the current setting.
func (s *ChaosState) Set(failRate float64, latencyMs int, mode ChaosMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if failRate < 0 {
		failRate = 0
	}
	if failRate > 1 {
		failRate = 1
	}
	s.FailRate, s.LatencyMs, s.Mode = failRate, latencyMs, mode
}

// Get reads the current setting.
func (s *ChaosState) Get() (float64, int, ChaosMode) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.FailRate, s.LatencyMs, s.Mode
}

// Chaos applies the state to every request in the group it is installed on.
func Chaos(state *ChaosState) gin.HandlerFunc {
	return func(c *gin.Context) {
		rate, latency, mode := state.Get()
		if latency > 0 {
			time.Sleep(time.Duration(latency) * time.Millisecond)
		}
		if rate > 0 && rand.Float64() < rate { //nolint:gosec // demo only, not security-relevant
			// Read the request before refusing it. Answering while the client
			// is still uploading makes the server close the connection and the
			// browser sees a truncated response instead of this envelope.
			_, _ = io.Copy(io.Discard, io.LimitReader(c.Request.Body, 8<<20))
			switch mode {
			case ChaosTimeout:
				time.Sleep(8 * time.Second)
				httpx.Fail(c, httpx.Domain(httpx.ReasonChaos, httpx.MsgChaos, http.StatusServiceUnavailable))
			case ChaosDrop:
				if conn, _, err := c.Writer.Hijack(); err == nil {
					_ = conn.Close()
				}
				c.Abort()
			default:
				httpx.Fail(c, httpx.Domain(httpx.ReasonChaos, httpx.MsgChaos, http.StatusInternalServerError))
			}
			return
		}
		c.Next()
	}
}
