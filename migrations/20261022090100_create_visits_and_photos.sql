-- +goose Up
CREATE TABLE visits (
  id           TEXT PRIMARY KEY,              -- v_ULID, generated on the device
  agent_id     TEXT NOT NULL REFERENCES agents(id),
  shop_name    TEXT NOT NULL,
  owner_name   TEXT NOT NULL,
  phone        TEXT NOT NULL,
  lat          DOUBLE PRECISION,
  lng          DOUBLE PRECISION,
  version      INTEGER NOT NULL DEFAULT 1,
  captured_at  TIMESTAMPTZ NOT NULL,          -- device clock, when the agent submitted
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Equality on agent first, then the range we page over.
CREATE INDEX idx_visits_agent_created ON visits (agent_id, created_at DESC, id DESC);
CREATE INDEX idx_visits_created ON visits (created_at DESC, id DESC);

CREATE TRIGGER visits_updated_at BEFORE UPDATE ON visits
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TABLE photos (
  id            TEXT PRIMARY KEY,             -- ph_ULID
  visit_id      TEXT NOT NULL REFERENCES visits(id) ON DELETE CASCADE,
  bytes         INTEGER NOT NULL CHECK (bytes > 0),
  content_hash  TEXT NOT NULL,
  content_type  TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_photos_visit ON photos (visit_id, created_at);

-- +goose Down
DROP TABLE photos;
DROP TABLE visits;
