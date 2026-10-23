// Package app assembles the Gin engine: middleware in a deliberate order,
// then each feature's Register on the right group.
package app

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/env"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/features/auth"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/features/control"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/features/photos"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/features/telemetry"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/features/visits"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/middleware"
)

// Deps is everything the engine needs. Tests build it against an embedded
// database; main builds it against the real one.
type Deps struct {
	Config env.Config
	Pool   *pgxpool.Pool
	Log    *slog.Logger
	Chaos  *middleware.ChaosState
}

// Engine builds the router. Order of middleware and of feature registration
// is load-bearing and commented inline.
func Engine(d Deps) (*gin.Engine, error) {
	if d.Config.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	gin.EnableJsonDecoderDisallowUnknownFields()

	e := gin.New()
	// Recover first so a panic anywhere below still renders the envelope.
	e.Use(middleware.Recover())
	// Request id before logging so every log line carries it.
	e.Use(middleware.RequestID())
	e.Use(middleware.Logger(d.Log))
	e.Use(cors.New(cors.Config{
		AllowOrigins:  strings.Split(d.Config.CORSOrigin, ","),
		AllowMethods:  []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:  []string{"Authorization", "Content-Type", "Idempotency-Key", "X-Request-Id"},
		ExposeHeaders: []string{"X-Request-Id", "Retry-After"},
	}))

	// The online probe: no auth, no body, no chaos. A cached or failing
	// probe would lie to the sync engine, so it sits outside everything.
	e.GET("/api/v1/ping", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	e.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	api := e.Group("/api/v1")
	if d.Config.DemoControl && d.Chaos != nil {
		// Chaos applies to every real API route, including login, so the
		// room sees the login retry too. The ping above is deliberately exempt.
		api.Use(middleware.Chaos(d.Chaos))
	}

	authSvc := auth.NewService(d.Pool, d.Config.JWTSecret)
	// Public: sign-in only.
	auth.RegisterPublic(api, authSvc)

	// Protected: everything else. Auth before rate limit so the limit is per
	// agent, not per IP. The idempotency middleware only captures the header
	// and body; the service writes the key inside its own transaction.
	protected := api.Group("")
	protected.Use(middleware.Auth(d.Config.JWTSecret))
	protected.Use(middleware.RateLimit(120, 30))
	protected.Use(middleware.Idempotency())

	auth.RegisterProtected(protected, authSvc)
	visits.Register(protected, visits.NewService(d.Pool))
	photoSvc, err := photos.NewService(d.Pool, d.Config.DataDir)
	if err != nil {
		return nil, err
	}
	photos.Register(protected, photoSvc)
	telemetry.Register(protected, telemetry.NewService(d.Pool))

	if d.Config.DemoControl && d.Chaos != nil {
		// Registered last and outside /api/v1 so it can never collide with a
		// real route.
		control.Register(e, d.Chaos)
	}
	return e, nil
}
