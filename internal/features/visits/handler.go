package visits

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/httpx"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/middleware"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/pagination"
)

// handler is thin: bind, call the service, write the result.
type handler struct{ svc *Service }

func (h handler) create(c *gin.Context) {
	var in CreateRequest
	// The idempotency middleware may have consumed the body; bind from its copy.
	if raw, ok := middleware.RawBody(c); ok {
		if err := json.Unmarshal(raw, &in); err != nil {
			httpx.Fail(c, httpx.Validation(map[string][]string{"body": {"must be valid JSON"}}))
			return
		}
		if err := binding().Struct(in); err != nil {
			httpx.Fail(c, err)
			return
		}
	} else if !httpx.Bind(c, &in) {
		return
	}
	stored, _, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Raw(c, stored.Status, stored.Body)
}

func (h handler) get(c *gin.Context) {
	v, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, v)
}

func (h handler) update(c *gin.Context) {
	var in UpdateRequest
	if !httpx.Bind(c, &in) {
		return
	}
	v, err := h.svc.Update(c.Request.Context(), c.Param("id"), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, v)
}

func (h handler) list(c *gin.Context) {
	p, err := h.svc.List(c.Request.Context(), c.Query("cursor"), pagination.Limit(c.Query("limit")))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OKMeta(c, p.Items, p.Meta)
}

func (h handler) stats(c *gin.Context) {
	s, err := h.svc.Stats(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, s)
}

// Register mounts the feature on the authenticated group. Gin resolves the
// static /visits/stats ahead of /visits/:id whatever the order, so the only
// ordering that matters here is that this group already carries Auth.
func Register(rg *gin.RouterGroup, svc *Service) {
	h := handler{svc}
	rg.GET("/visits/stats", h.stats)
	rg.GET("/visits", h.list)
	rg.POST("/visits", h.create)
	rg.GET("/visits/:id", h.get)
	rg.PUT("/visits/:id", h.update)
}
