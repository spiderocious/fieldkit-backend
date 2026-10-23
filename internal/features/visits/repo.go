package visits

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/pagination"
)

// repo is SQL only. No decisions live here.
type repo struct{ pool *pgxpool.Pool }

const visitColumns = `
	v.id, v.agent_id, v.shop_name, v.owner_name, v.phone, v.lat, v.lng, v.version,
	(SELECT count(*) FROM photos p WHERE p.visit_id = v.id)::int AS photo_count,
	v.captured_at, v.created_at, v.updated_at`

func scanVisit(row pgx.Row) (Visit, error) {
	var v Visit
	err := row.Scan(&v.ID, &v.AgentID, &v.ShopName, &v.OwnerName, &v.Phone, &v.Lat, &v.Lng, &v.Version,
		&v.PhotoCount, &v.CapturedAt, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

func (r repo) insert(ctx context.Context, tx pgx.Tx, agentID string, in CreateRequest) (Visit, error) {
	return scanVisit(tx.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO visits (id, agent_id, shop_name, owner_name, phone, lat, lng, captured_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING *
		)
		SELECT `+visitColumns+` FROM ins v`,
		in.ID, agentID, in.ShopName, in.OwnerName, in.Phone, in.Lat, in.Lng, in.CapturedAt))
}

func (r repo) getByID(ctx context.Context, id string) (Visit, error) {
	return scanVisit(r.pool.QueryRow(ctx, `SELECT `+visitColumns+` FROM visits v WHERE v.id = $1`, id))
}

// updateWithVersion applies the update only when the row is still at
// baseVersion. Zero rows means someone else wrote first.
func (r repo) updateWithVersion(ctx context.Context, id string, baseVersion int, in UpdateRequest) (Visit, bool, error) {
	v, err := scanVisit(r.pool.QueryRow(ctx, `
		WITH upd AS (
			UPDATE visits SET
				shop_name  = COALESCE($3, shop_name),
				owner_name = COALESCE($4, owner_name),
				phone      = COALESCE($5, phone),
				lat        = COALESCE($6, lat),
				lng        = COALESCE($7, lng),
				version    = version + 1
			WHERE id = $1 AND version = $2
			RETURNING *
		)
		SELECT `+visitColumns+` FROM upd v`,
		id, baseVersion, in.ShopName, in.OwnerName, in.Phone, in.Lat, in.Lng))
	if errors.Is(err, pgx.ErrNoRows) {
		return Visit{}, false, nil
	}
	return v, err == nil, err
}

// list pages newest first by (created_at, id). It fetches limit+1 rows to
// learn whether there is another page without a second query.
func (r repo) list(ctx context.Context, agentID string, cur pagination.Cursor, hasCur bool, limit int) ([]Visit, bool, error) {
	var rows pgx.Rows
	var err error
	if hasCur {
		rows, err = r.pool.Query(ctx, `
			SELECT `+visitColumns+` FROM visits v
			WHERE v.agent_id = $1 AND (v.created_at, v.id) < ($2::timestamptz, $3)
			ORDER BY v.created_at DESC, v.id DESC LIMIT $4`,
			agentID, cur.LastSortKey, cur.LastID, limit+1)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT `+visitColumns+` FROM visits v
			WHERE v.agent_id = $1
			ORDER BY v.created_at DESC, v.id DESC LIMIT $2`, agentID, limit+1)
	}
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := make([]Visit, 0, limit)
	for rows.Next() {
		v, err := scanVisit(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, v)
	}
	if rows.Err() != nil {
		return nil, false, rows.Err()
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

func (r repo) stats(ctx context.Context) (Stats, error) {
	var s Stats
	err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM visits)::int,
			(SELECT count(*) FROM visits WHERE created_at >= date_trunc('day', now()))::int,
			(SELECT count(*) FROM photos)::int`).Scan(&s.Total, &s.ReceivedToday, &s.Photos)
	if err != nil {
		return Stats{}, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT a.id, a.name, count(v.id)::int
		FROM agents a LEFT JOIN visits v ON v.agent_id = a.id
		GROUP BY a.id, a.name ORDER BY a.name`)
	if err != nil {
		return Stats{}, err
	}
	defer rows.Close()
	s.ByAgent = []AgentCount{}
	for rows.Next() {
		var ac AgentCount
		if err := rows.Scan(&ac.AgentID, &ac.Name, &ac.Count); err != nil {
			return Stats{}, err
		}
		s.ByAgent = append(s.ByAgent, ac)
	}
	return s, rows.Err()
}
