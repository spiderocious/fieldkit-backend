package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/app"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/env"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/httpx"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/middleware"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/testutil"
)

// These are contract tests: every response is decoded into the wire struct
// with unknown fields disallowed, so drift between the serialiser and the
// documented shape fails here before a frontend sees it.

var (
	tdb    *testutil.DB
	engine *gin.Engine
	chaos  *middleware.ChaosState
)

const (
	agentID = "ag_01TESTAGENT0000000000000000"
	phone   = "08030000001"
	pin     = "1234"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	db, err := testutil.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	tdb = db
	chaos = middleware.NewChaos()
	dataDir, _ := os.MkdirTemp("", "fieldkit-data-")
	engine, err = app.Engine(app.Deps{
		Config: env.Config{Env: "test", JWTSecret: strings.Repeat("s", 32), DataDir: dataDir, DemoControl: true, CORSOrigin: "*"},
		Pool:   db.Pool,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Chaos:  chaos,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	db.Stop()
	os.Exit(code)
}

// ---- helpers ----

func seedAgent(t *testing.T) {
	t.Helper()
	tdb.Truncate(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.MinCost)
	_, err := tdb.Pool.Exec(context.Background(),
		`INSERT INTO agents (id, phone, name, pin_hash) VALUES ($1, $2, 'Test Agent', $3)`, agentID, phone, string(hash))
	if err != nil {
		t.Fatal(err)
	}
}

func token(t *testing.T) string {
	t.Helper()
	tok, err := middleware.SignToken(strings.Repeat("s", 32), agentID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

type req struct {
	method, path string
	body         any
	headers      map[string]string
}

func do(t *testing.T, r req) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if r.body != nil {
		b, _ := json.Marshal(r.body)
		body = bytes.NewReader(b)
	}
	hr := httptest.NewRequest(r.method, r.path, body)
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Authorization", "Bearer "+token(t))
	for k, v := range r.headers {
		hr.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, hr)
	return w
}

// decode enforces the contract: unknown fields are an error.
func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("decode %T: %v\nbody: %s", out, err, w.Body.String())
	}
	return out
}

type visitWire struct {
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

type dataEnvelope[T any] struct {
	Data T `json:"data"`
}

type pageEnvelope[T any] struct {
	Data []T `json:"data"`
	Meta struct {
		NextCursor *string `json:"next_cursor"`
		HasMore    bool    `json:"has_more"`
	} `json:"meta"`
}

type errorEnvelope struct {
	Error struct {
		Reason      string              `json:"reason"`
		Message     string              `json:"message"`
		Severity    int                 `json:"severity"`
		FieldErrors map[string][]string `json:"field_errors"`
		Data        json.RawMessage     `json:"data"`
		RequestID   string              `json:"request_id"`
	} `json:"error"`
}

func newVisit(id string) map[string]any {
	return map[string]any{
		"id": id, "shop_name": "Mama Tunde Provisions", "owner_name": "Adebayo Ojo",
		"phone": "08131000001", "lat": 7.2508, "lng": 5.2103, "captured_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// ---- auth ----

func TestLogin(t *testing.T) {
	seedAgent(t)

	t.Run("valid pin returns a token and the agent", func(t *testing.T) {
		w := do(t, req{method: "POST", path: "/api/v1/auth/login", body: map[string]string{"phone": phone, "pin": pin}})
		if w.Code != 200 {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		out := decode[dataEnvelope[struct {
			Token string `json:"token"`
			Agent struct {
				ID, Name, Phone string
			} `json:"agent"`
		}]](t, w)
		if out.Data.Token == "" || out.Data.Agent.ID != agentID {
			t.Fatalf("unexpected: %+v", out)
		}
	})

	t.Run("wrong pin and unknown phone are the same error", func(t *testing.T) {
		a := do(t, req{method: "POST", path: "/api/v1/auth/login", body: map[string]string{"phone": phone, "pin": "9999"}})
		b := do(t, req{method: "POST", path: "/api/v1/auth/login", body: map[string]string{"phone": "08000000000", "pin": pin}})
		for _, w := range []*httptest.ResponseRecorder{a, b} {
			if w.Code != 401 {
				t.Fatalf("status %d", w.Code)
			}
			e := decode[errorEnvelope](t, w)
			if e.Error.Reason != string(httpx.ReasonInvalidCredentials) || e.Error.Message == "" {
				t.Fatalf("envelope %+v", e)
			}
		}
	})

	t.Run("missing token is token_invalid", func(t *testing.T) {
		hr := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, hr)
		if w.Code != 401 || decode[errorEnvelope](t, w).Error.Reason != string(httpx.ReasonTokenInvalid) {
			t.Fatalf("status %d body %s", w.Code, w.Body)
		}
	})
}

// ---- visits ----

func TestCreateVisit(t *testing.T) {
	seedAgent(t)

	t.Run("creates and returns the full visit", func(t *testing.T) {
		w := do(t, req{method: "POST", path: "/api/v1/visits", body: newVisit("v_01A"), headers: map[string]string{"Idempotency-Key": "k-create-1"}})
		if w.Code != 201 {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		v := decode[dataEnvelope[visitWire]](t, w).Data
		if v.ID != "v_01A" || v.Version != 1 || v.AgentID != agentID || v.PhotoCount != 0 {
			t.Fatalf("visit %+v", v)
		}
	})

	t.Run("same key and body replays the stored response and creates nothing", func(t *testing.T) {
		body := newVisit("v_01B")
		first := do(t, req{method: "POST", path: "/api/v1/visits", body: body, headers: map[string]string{"Idempotency-Key": "k-replay"}})
		second := do(t, req{method: "POST", path: "/api/v1/visits", body: body, headers: map[string]string{"Idempotency-Key": "k-replay"}})
		if first.Code != 201 || second.Code != 201 {
			t.Fatalf("codes %d %d", first.Code, second.Code)
		}
		if first.Body.String() != second.Body.String() {
			t.Fatalf("replay differs:\n%s\n%s", first.Body, second.Body)
		}
		var n int
		_ = tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM visits WHERE id = 'v_01B'`).Scan(&n)
		if n != 1 {
			t.Fatalf("expected one row, got %d", n)
		}
	})

	t.Run("same key with a different body is idempotency_mismatch", func(t *testing.T) {
		do(t, req{method: "POST", path: "/api/v1/visits", body: newVisit("v_01C"), headers: map[string]string{"Idempotency-Key": "k-mismatch"}})
		changed := newVisit("v_01C")
		changed["shop_name"] = "Different Shop"
		w := do(t, req{method: "POST", path: "/api/v1/visits", body: changed, headers: map[string]string{"Idempotency-Key": "k-mismatch"}})
		if w.Code != 422 || decode[errorEnvelope](t, w).Error.Reason != string(httpx.ReasonIdempotencyMismatch) {
			t.Fatalf("status %d body %s", w.Code, w.Body)
		}
	})

	t.Run("same visit id without a key is already_exists", func(t *testing.T) {
		do(t, req{method: "POST", path: "/api/v1/visits", body: newVisit("v_01D")})
		w := do(t, req{method: "POST", path: "/api/v1/visits", body: newVisit("v_01D")})
		if w.Code != 409 || decode[errorEnvelope](t, w).Error.Reason != string(httpx.ReasonAlreadyExists) {
			t.Fatalf("status %d body %s", w.Code, w.Body)
		}
	})

	t.Run("validation returns every bad field", func(t *testing.T) {
		w := do(t, req{method: "POST", path: "/api/v1/visits", body: map[string]any{"id": "bad", "phone": "1"}})
		if w.Code != 400 {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		e := decode[errorEnvelope](t, w)
		if e.Error.Reason != string(httpx.ReasonValidation) || len(e.Error.FieldErrors) < 3 {
			t.Fatalf("envelope %+v", e)
		}
	})

	t.Run("unknown field is rejected", func(t *testing.T) {
		b := newVisit("v_01E")
		b["surprise"] = true
		w := do(t, req{method: "POST", path: "/api/v1/visits", body: b})
		if w.Code != 400 {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
	})
}

func TestUpdateVisit(t *testing.T) {
	seedAgent(t)
	do(t, req{method: "POST", path: "/api/v1/visits", body: newVisit("v_02A")})

	t.Run("matching base_version applies and bumps the version", func(t *testing.T) {
		w := do(t, req{method: "PUT", path: "/api/v1/visits/v_02A", body: map[string]any{"base_version": 1, "phone": "08131000099"}})
		if w.Code != 200 {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		v := decode[dataEnvelope[visitWire]](t, w).Data
		if v.Version != 2 || v.Phone != "08131000099" || v.ShopName != "Mama Tunde Provisions" {
			t.Fatalf("visit %+v", v)
		}
	})

	t.Run("stale base_version is version_conflict with the current record", func(t *testing.T) {
		w := do(t, req{method: "PUT", path: "/api/v1/visits/v_02A", body: map[string]any{"base_version": 1, "owner_name": "Someone Else"}})
		if w.Code != 409 {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		e := decode[errorEnvelope](t, w)
		if e.Error.Reason != string(httpx.ReasonVersionConflict) {
			t.Fatalf("reason %s", e.Error.Reason)
		}
		var cur visitWire
		if err := json.Unmarshal(e.Error.Data, &cur); err != nil || cur.Version != 2 || cur.OwnerName != "Adebayo Ojo" {
			t.Fatalf("current %+v err %v", cur, err)
		}
	})

	t.Run("another agent's visit is not_found, not forbidden", func(t *testing.T) {
		_, _ = tdb.Pool.Exec(context.Background(), `INSERT INTO agents (id, phone, name, pin_hash) VALUES ('ag_OTHER', '08039999999', 'Other', 'x')`)
		_, _ = tdb.Pool.Exec(context.Background(), `INSERT INTO visits (id, agent_id, shop_name, owner_name, phone, captured_at) VALUES ('v_OTHER', 'ag_OTHER', 's', 'o', '08131', now())`)
		w := do(t, req{method: "GET", path: "/api/v1/visits/v_OTHER"})
		if w.Code != 404 {
			t.Fatalf("status %d", w.Code)
		}
	})
}

func TestListVisits(t *testing.T) {
	seedAgent(t)
	for i := 0; i < 5; i++ {
		do(t, req{method: "POST", path: "/api/v1/visits", body: newVisit(fmt.Sprintf("v_03%d", i))})
	}

	first := do(t, req{method: "GET", path: "/api/v1/visits?limit=2"})
	p1 := decode[pageEnvelope[visitWire]](t, first)
	if len(p1.Data) != 2 || !p1.Meta.HasMore || p1.Meta.NextCursor == nil {
		t.Fatalf("page1 %+v", p1.Meta)
	}
	second := do(t, req{method: "GET", path: "/api/v1/visits?limit=2&cursor=" + *p1.Meta.NextCursor})
	p2 := decode[pageEnvelope[visitWire]](t, second)
	if len(p2.Data) != 2 || !p2.Meta.HasMore {
		t.Fatalf("page2 %+v", p2.Meta)
	}
	third := do(t, req{method: "GET", path: "/api/v1/visits?limit=2&cursor=" + *p2.Meta.NextCursor})
	p3 := decode[pageEnvelope[visitWire]](t, third)
	if len(p3.Data) != 1 || p3.Meta.HasMore || p3.Meta.NextCursor != nil {
		t.Fatalf("page3 %+v", p3.Meta)
	}
	seen := map[string]bool{}
	for _, page := range [][]visitWire{p1.Data, p2.Data, p3.Data} {
		for _, v := range page {
			if seen[v.ID] {
				t.Fatalf("duplicate across pages: %s", v.ID)
			}
			seen[v.ID] = true
		}
	}

	t.Run("stats is served ahead of the :id route", func(t *testing.T) {
		w := do(t, req{method: "GET", path: "/api/v1/visits/stats"})
		if w.Code != 200 {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		s := decode[dataEnvelope[struct {
			Total         int `json:"total"`
			ReceivedToday int `json:"received_today"`
			Photos        int `json:"photos"`
			ByAgent       []struct {
				AgentID string `json:"agent_id"`
				Name    string `json:"name"`
				Count   int    `json:"count"`
			} `json:"by_agent"`
		}]](t, w).Data
		if s.Total != 5 || len(s.ByAgent) != 1 || s.ByAgent[0].Count != 5 {
			t.Fatalf("stats %+v", s)
		}
	})
}

// ---- photos ----

func TestPhotos(t *testing.T) {
	seedAgent(t)
	do(t, req{method: "POST", path: "/api/v1/visits", body: newVisit("v_04A")})

	upload := func(size int) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		part, _ := mw.CreateFormFile("file", "photo.jpg")
		_, _ = part.Write(bytes.Repeat([]byte{0xFF}, size))
		_ = mw.Close()
		hr := httptest.NewRequest("POST", "/api/v1/visits/v_04A/photos", &buf)
		hr.Header.Set("Content-Type", mw.FormDataContentType())
		hr.Header.Set("Authorization", "Bearer "+token(t))
		// Devices send a key on every write, uploads included. The
		// idempotency middleware must leave the multipart stream readable.
		hr.Header.Set("Idempotency-Key", "k-photo-"+strconv.Itoa(size))
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, hr)
		return w
	}

	t.Run("stores a photo and serves it back", func(t *testing.T) {
		w := upload(1024)
		if w.Code != 201 {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		p := decode[dataEnvelope[struct {
			ID          string `json:"id"`
			VisitID     string `json:"visit_id"`
			Bytes       int    `json:"bytes"`
			ContentHash string `json:"content_hash"`
			ContentType string `json:"content_type"`
			CreatedAt   string `json:"created_at"`
		}]](t, w).Data
		if p.Bytes != 1024 || p.VisitID != "v_04A" {
			t.Fatalf("photo %+v", p)
		}
		get := do(t, req{method: "GET", path: "/api/v1/visits/v_04A/photos/" + p.ID})
		if get.Code != 200 || get.Body.Len() != 1024 {
			t.Fatalf("serve %d len %d", get.Code, get.Body.Len())
		}
		v := decode[dataEnvelope[visitWire]](t, do(t, req{method: "GET", path: "/api/v1/visits/v_04A"})).Data
		if v.PhotoCount != 1 {
			t.Fatalf("photo_count %d", v.PhotoCount)
		}
	})

	t.Run("over 2 MB is payload_too_large", func(t *testing.T) {
		w := upload(2<<20 + 10)
		if w.Code != 413 {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
	})
}

// ---- telemetry ----

func TestTelemetry(t *testing.T) {
	seedAgent(t)
	w := do(t, req{method: "POST", path: "/api/v1/telemetry", body: map[string]any{"events": []map[string]any{
		{"kind": "queue.drained", "payload": map[string]int{"count": 3}, "occurred_at": time.Now().UTC()},
		{"kind": "sync.failed", "occurred_at": time.Now().UTC()},
	}}})
	if w.Code != 202 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	out := decode[dataEnvelope[struct {
		Accepted int `json:"accepted"`
		Skipped  int `json:"skipped"`
	}]](t, w).Data
	if out.Accepted != 2 || out.Skipped != 0 {
		t.Fatalf("%+v", out)
	}
}

// ---- chaos ----

func TestChaosControl(t *testing.T) {
	seedAgent(t)
	w := do(t, req{method: "POST", path: "/__control", body: map[string]any{"fail_rate": 1, "mode": "500"}})
	if w.Code != 204 {
		t.Fatalf("set: %d %s", w.Code, w.Body)
	}
	t.Cleanup(func() { chaos.Set(0, 0, middleware.ChaosNone) })

	fail := do(t, req{method: "GET", path: "/api/v1/visits"})
	if fail.Code != 500 || decode[errorEnvelope](t, fail).Error.Reason != string(httpx.ReasonChaos) {
		t.Fatalf("expected injected failure, got %d %s", fail.Code, fail.Body)
	}
	ping := do(t, req{method: "GET", path: "/api/v1/ping"})
	if ping.Code != 204 {
		t.Fatalf("ping must be exempt from chaos, got %d", ping.Code)
	}
}

// ---- rate limit ----

func TestRateLimitSetsRetryAfter(t *testing.T) {
	seedAgent(t)
	var last *httptest.ResponseRecorder
	for i := 0; i < 40; i++ {
		last = do(t, req{method: "GET", path: "/api/v1/auth/me"})
		if last.Code == 429 {
			break
		}
	}
	if last.Code != 429 {
		t.Fatalf("never rate limited")
	}
	if last.Header().Get("Retry-After") == "" {
		t.Fatalf("Retry-After missing")
	}
	if decode[errorEnvelope](t, last).Error.Reason != string(httpx.ReasonRateLimited) {
		t.Fatalf("reason")
	}
}
