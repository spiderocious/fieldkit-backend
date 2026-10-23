// Package visits is the core feature: a shop visit captured on a device,
// created once under an idempotency key, and updated with version checks.
package visits

import "time"

// Visit is the wire shape. Field names are snake_case on the wire and are the
// contract the frontend types are copied from.
type Visit struct {
	ID         string    `json:"id"`
	AgentID    string    `json:"agent_id"`
	ShopName   string    `json:"shop_name"`
	OwnerName  string    `json:"owner_name"`
	Phone      string    `json:"phone"`
	Lat        *float64  `json:"lat"`
	Lng        *float64  `json:"lng"`
	Version    int       `json:"version"`
	PhotoCount int       `json:"photo_count"`
	CapturedAt time.Time `json:"captured_at"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// CreateRequest is the body for POST /visits. The id comes from the device so
// the visit has an identity before it has ever reached the server.
type CreateRequest struct {
	ID         string    `json:"id" binding:"required,startswith=v_,max=40"`
	ShopName   string    `json:"shop_name" binding:"required,max=120"`
	OwnerName  string    `json:"owner_name" binding:"required,max=120"`
	Phone      string    `json:"phone" binding:"required,min=7,max=20"`
	Lat        *float64  `json:"lat" binding:"omitempty,latitude"`
	Lng        *float64  `json:"lng" binding:"omitempty,longitude"`
	CapturedAt time.Time `json:"captured_at" binding:"required"`
}

// UpdateRequest is the body for PUT /visits/:id. Only present fields change.
// BaseVersion is the version the client read before editing.
type UpdateRequest struct {
	BaseVersion int      `json:"base_version" binding:"required,min=1"`
	ShopName    *string  `json:"shop_name" binding:"omitempty,max=120"`
	OwnerName   *string  `json:"owner_name" binding:"omitempty,max=120"`
	Phone       *string  `json:"phone" binding:"omitempty,min=7,max=20"`
	Lat         *float64 `json:"lat" binding:"omitempty,latitude"`
	Lng         *float64 `json:"lng" binding:"omitempty,longitude"`
}

// Stats is the ops summary.
type Stats struct {
	Total         int          `json:"total"`
	ReceivedToday int          `json:"received_today"`
	Photos        int          `json:"photos"`
	ByAgent       []AgentCount `json:"by_agent"`
}

// AgentCount is one row of the per-agent breakdown.
type AgentCount struct {
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
	Count   int    `json:"count"`
}
