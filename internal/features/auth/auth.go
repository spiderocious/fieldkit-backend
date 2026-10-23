// Package auth signs agents in. Phone plus PIN, returning a bearer token.
package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/httpx"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/middleware"
)

// ---- types ----

// Agent is the wire shape of an agent. The PIN hash never leaves the repo.
type Agent struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

// LoginRequest is the sign-in body.
type LoginRequest struct {
	Phone string `json:"phone" binding:"required,min=7,max=20"`
	PIN   string `json:"pin" binding:"required,len=4,numeric"`
}

// LoginResponse carries the token and the agent.
type LoginResponse struct {
	Token string `json:"token"`
	Agent Agent  `json:"agent"`
}

// ---- repo ----

type repo struct{ pool *pgxpool.Pool }

type agentRow struct {
	Agent
	PINHash string
}

func (r repo) findByPhone(ctx context.Context, phone string) (agentRow, error) {
	var a agentRow
	err := r.pool.QueryRow(ctx, `SELECT id, name, phone, pin_hash FROM agents WHERE phone = $1`, phone).
		Scan(&a.ID, &a.Name, &a.Phone, &a.PINHash)
	return a, err
}

// FindByID is used by other features to resolve an agent.
func (r repo) findByID(ctx context.Context, id string) (Agent, error) {
	var a Agent
	err := r.pool.QueryRow(ctx, `SELECT id, name, phone FROM agents WHERE id = $1`, id).Scan(&a.ID, &a.Name, &a.Phone)
	return a, err
}

// ---- service ----

// Service holds auth logic. It never sees the Gin context.
type Service struct {
	repo   repo
	secret string
	ttl    time.Duration
}

// NewService wires the service.
func NewService(pool *pgxpool.Pool, secret string) *Service {
	return &Service{repo: repo{pool}, secret: secret, ttl: 30 * 24 * time.Hour}
}

// Login verifies the PIN and issues a token. A wrong phone and a wrong PIN
// are the same error, so the endpoint cannot be used to enumerate phones.
func (s *Service) Login(ctx context.Context, in LoginRequest) (LoginResponse, error) {
	a, err := s.repo.findByPhone(ctx, in.Phone)
	if errors.Is(err, pgx.ErrNoRows) {
		return LoginResponse{}, invalidCredentials()
	}
	if err != nil {
		return LoginResponse{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(a.PINHash), []byte(in.PIN)) != nil {
		return LoginResponse{}, invalidCredentials()
	}
	tok, err := middleware.SignToken(s.secret, a.ID, s.ttl)
	if err != nil {
		return LoginResponse{}, err
	}
	return LoginResponse{Token: tok, Agent: a.Agent}, nil
}

// Me returns the agent for the authenticated id.
func (s *Service) Me(ctx context.Context) (Agent, error) {
	a, err := s.repo.findByID(ctx, middleware.AgentIDFrom(ctx))
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, httpx.Domain(httpx.ReasonTokenInvalid, httpx.MsgTokenInvalid, http.StatusUnauthorized)
	}
	return a, err
}

func invalidCredentials() *httpx.DomainError {
	return httpx.Domain(httpx.ReasonInvalidCredentials, httpx.MsgInvalidCredentials, http.StatusUnauthorized)
}

// ---- handler + routes ----

type handler struct{ svc *Service }

func (h handler) login(c *gin.Context) {
	var in LoginRequest
	if !httpx.Bind(c, &in) {
		return
	}
	out, err := h.svc.Login(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, out)
}

func (h handler) me(c *gin.Context) {
	a, err := h.svc.Me(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, a)
}

// RegisterPublic mounts the routes that need no token.
func RegisterPublic(rg *gin.RouterGroup, svc *Service) {
	h := handler{svc}
	rg.POST("/auth/login", h.login)
}

// RegisterProtected mounts the routes that need a token.
func RegisterProtected(rg *gin.RouterGroup, svc *Service) {
	h := handler{svc}
	rg.GET("/auth/me", h.me)
}
