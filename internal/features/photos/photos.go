// Package photos stores visit photos on local disk and records them.
package photos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/httpx"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/ids"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/middleware"
)

// MaxBytes is the upload ceiling. The device compresses to well under it.
const MaxBytes = 2 << 20

// Photo is the wire shape.
type Photo struct {
	ID          string    `json:"id"`
	VisitID     string    `json:"visit_id"`
	Bytes       int       `json:"bytes"`
	ContentHash string    `json:"content_hash"`
	ContentType string    `json:"content_type"`
	CreatedAt   time.Time `json:"created_at"`
}

type repo struct{ pool *pgxpool.Pool }

func (r repo) visitOwner(ctx context.Context, visitID string) (string, error) {
	var owner string
	err := r.pool.QueryRow(ctx, `SELECT agent_id FROM visits WHERE id = $1`, visitID).Scan(&owner)
	return owner, err
}

func (r repo) insert(ctx context.Context, p Photo) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO photos (id, visit_id, bytes, content_hash, content_type) VALUES ($1, $2, $3, $4, $5)`,
		p.ID, p.VisitID, p.Bytes, p.ContentHash, p.ContentType)
	return err
}

func (r repo) get(ctx context.Context, visitID, id string) (Photo, error) {
	var p Photo
	err := r.pool.QueryRow(ctx, `
		SELECT id, visit_id, bytes, content_hash, content_type, created_at FROM photos WHERE id = $1 AND visit_id = $2`, id, visitID).
		Scan(&p.ID, &p.VisitID, &p.Bytes, &p.ContentHash, &p.ContentType, &p.CreatedAt)
	return p, err
}

// Service stores files under dataDir/photos/<visit>/<id>.
type Service struct {
	repo    repo
	dataDir string
}

// NewService wires the service and makes sure the directory exists.
func NewService(pool *pgxpool.Pool, dataDir string) (*Service, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "photos"), 0o750); err != nil {
		return nil, fmt.Errorf("photos: %w", err)
	}
	return &Service{repo: repo{pool}, dataDir: dataDir}, nil
}

func (s *Service) path(visitID, id string) string {
	return filepath.Join(s.dataDir, "photos", visitID, id)
}

// Store writes the stream to disk and records the row. The caller's
// ownership of the visit is checked first.
func (s *Service) Store(ctx context.Context, visitID, contentType string, body io.Reader) (Photo, error) {
	owner, err := s.repo.visitOwner(ctx, visitID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != middleware.AgentIDFrom(ctx)) {
		return Photo{}, httpx.NotFound(httpx.MsgVisitNotFound)
	}
	if err != nil {
		return Photo{}, err
	}

	id := ids.New(ids.Photo)
	if err := os.MkdirAll(filepath.Join(s.dataDir, "photos", visitID), 0o750); err != nil {
		return Photo{}, err
	}
	f, err := os.Create(s.path(visitID, id))
	if err != nil {
		return Photo{}, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(body, MaxBytes+1))
	_ = f.Close()
	if err != nil {
		_ = os.Remove(s.path(visitID, id))
		return Photo{}, err
	}
	if n > MaxBytes {
		_ = os.Remove(s.path(visitID, id))
		return Photo{}, httpx.Domain(httpx.ReasonPayloadTooLarge, httpx.MsgPhotoTooLarge, http.StatusRequestEntityTooLarge)
	}
	if n == 0 {
		_ = os.Remove(s.path(visitID, id))
		return Photo{}, httpx.Validation(map[string][]string{"file": {"must not be empty"}})
	}
	p := Photo{ID: id, VisitID: visitID, Bytes: int(n), ContentHash: hex.EncodeToString(h.Sum(nil)), ContentType: contentType, CreatedAt: time.Now()}
	if err := s.repo.insert(ctx, p); err != nil {
		_ = os.Remove(s.path(visitID, id))
		return Photo{}, err
	}
	return p, nil
}

// Open returns the photo metadata and its file for serving.
func (s *Service) Open(ctx context.Context, visitID, id string) (Photo, *os.File, error) {
	owner, err := s.repo.visitOwner(ctx, visitID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != middleware.AgentIDFrom(ctx)) {
		return Photo{}, nil, httpx.NotFound(httpx.MsgVisitNotFound)
	}
	if err != nil {
		return Photo{}, nil, err
	}
	p, err := s.repo.get(ctx, visitID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Photo{}, nil, httpx.NotFound(httpx.MsgPhotoNotFound)
	}
	if err != nil {
		return Photo{}, nil, err
	}
	f, err := os.Open(s.path(visitID, id))
	if err != nil {
		return Photo{}, nil, httpx.NotFound(httpx.MsgPhotoNotFound)
	}
	return p, f, nil
}

type handler struct{ svc *Service }

func (h handler) upload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBytes+1024)
	fh, err := c.FormFile("file")
	if err != nil {
		httpx.Fail(c, httpx.Validation(map[string][]string{"file": {"required multipart field"}}))
		return
	}
	f, err := fh.Open()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer func() { _ = f.Close() }()
	ct := fh.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	p, err := h.svc.Store(c.Request.Context(), c.Param("id"), ct, f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, p)
}

func (h handler) serve(c *gin.Context) {
	p, f, err := h.svc.Open(c.Request.Context(), c.Param("id"), c.Param("photoId"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer func() { _ = f.Close() }()
	c.Header("Cache-Control", "private, max-age=31536000, immutable")
	c.DataFromReader(http.StatusOK, int64(p.Bytes), p.ContentType, f, nil)
}

// Register mounts the feature on the authenticated group.
func Register(rg *gin.RouterGroup, svc *Service) {
	h := handler{svc}
	rg.POST("/visits/:id/photos", h.upload)
	rg.GET("/visits/:id/photos/:photoId", h.serve)
}
