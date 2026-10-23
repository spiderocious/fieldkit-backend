package visits

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/db"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/httpx"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/middleware"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/pagination"
)

// Service owns visit business logic. It takes a context and typed inputs and
// returns values; a domain rejection is a *httpx.DomainError, never a panic.
type Service struct {
	pool *pgxpool.Pool
	repo repo
}

// NewService wires the service.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, repo: repo{pool}} }

// endpoint names the idempotency scope so a key reused on a different route
// is a mismatch, not a replay.
const createEndpoint = "POST /visits"

// Create inserts a visit inside the idempotency transaction. The returned
// Stored is the exact envelope to write, and replayed says whether it came
// from the key table rather than a fresh insert.
func (s *Service) Create(ctx context.Context, in CreateRequest) (middleware.Stored, bool, error) {
	agentID := middleware.AgentIDFrom(ctx)
	stored, replayed, err := middleware.Execute(ctx, s.pool, createEndpoint, func(tx pgx.Tx) (int, any, error) {
		v, err := s.repo.insert(ctx, tx, agentID, in)
		if err != nil {
			if db.IsUniqueViolation(err, "visits_pkey") {
				return 0, nil, httpx.Domain(httpx.ReasonAlreadyExists, httpx.MsgVisitExists, http.StatusConflict).
					WithDiagnostic("visit id already stored without an idempotency key")
			}
			return 0, nil, err
		}
		return http.StatusCreated, v, nil
	})
	return stored, replayed, err
}

// Get returns one visit owned by the caller.
func (s *Service) Get(ctx context.Context, id string) (Visit, error) {
	v, err := s.repo.getByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && v.AgentID != middleware.AgentIDFrom(ctx)) {
		return Visit{}, httpx.NotFound(httpx.MsgVisitNotFound)
	}
	return v, err
}

// Update applies a partial edit with a version check. On conflict the
// current record rides back in the error so the client can reconcile.
func (s *Service) Update(ctx context.Context, id string, in UpdateRequest) (Visit, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return Visit{}, err
	}
	v, ok, err := s.repo.updateWithVersion(ctx, id, in.BaseVersion, in)
	if err != nil {
		return Visit{}, err
	}
	if ok {
		return v, nil
	}
	current, err := s.repo.getByID(ctx, id)
	if err != nil {
		return Visit{}, err
	}
	return Visit{}, httpx.Domain(httpx.ReasonVersionConflict, httpx.MsgVisitConflict, http.StatusConflict).
		WithData(current).
		WithDiagnostic("base_version behind server version")
}

// Page is a page of visits with its pagination meta.
type Page struct {
	Items []Visit
	Meta  pagination.Meta
}

// List pages the caller's visits newest first.
func (s *Service) List(ctx context.Context, cursor string, limit int) (Page, error) {
	cur, has := pagination.Decode(cursor)
	items, more, err := s.repo.list(ctx, middleware.AgentIDFrom(ctx), cur, has, limit)
	if err != nil {
		return Page{}, err
	}
	p := Page{Items: items, Meta: pagination.Meta{HasMore: more}}
	if more {
		last := items[len(items)-1]
		next := pagination.Encode(pagination.Cursor{LastID: last.ID, LastSortKey: last.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00")})
		p.Meta.NextCursor = &next
	}
	return p, nil
}

// Stats returns the ops summary across all agents.
func (s *Service) Stats(ctx context.Context) (Stats, error) { return s.repo.stats(ctx) }
