// Package control is the demo chaos switch. It is registered only when
// DEMO_CONTROL is set, on its own group outside /api/v1.
package control

import (
	"github.com/gin-gonic/gin"

	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/httpx"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/middleware"
)

// SetRequest is the body for POST /__control.
type SetRequest struct {
	FailRate  float64              `json:"fail_rate" binding:"min=0,max=1"`
	LatencyMs int                  `json:"latency_ms" binding:"min=0,max=30000"`
	Mode      middleware.ChaosMode `json:"mode" binding:"omitempty,oneof=none 500 timeout drop"`
}

// State is the wire shape of the current setting.
type State struct {
	FailRate  float64              `json:"fail_rate"`
	LatencyMs int                  `json:"latency_ms"`
	Mode      middleware.ChaosMode `json:"mode"`
}

type handler struct{ state *middleware.ChaosState }

func (h handler) get(c *gin.Context) {
	r, l, m := h.state.Get()
	httpx.OK(c, State{FailRate: r, LatencyMs: l, Mode: m})
}

func (h handler) set(c *gin.Context) {
	var in SetRequest
	if !httpx.Bind(c, &in) {
		return
	}
	mode := in.Mode
	if mode == "" {
		mode = middleware.Chaos500
	}
	h.state.Set(in.FailRate, in.LatencyMs, mode)
	httpx.NoContent(c)
}

// Register mounts the switch at /__control on the engine itself.
func Register(e *gin.Engine, state *middleware.ChaosState) {
	h := handler{state}
	e.GET("/__control", h.get)
	e.POST("/__control", h.set)
}
