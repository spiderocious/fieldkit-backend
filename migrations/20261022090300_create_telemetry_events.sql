-- +goose Up
CREATE TABLE telemetry_events (
  id           TEXT PRIMARY KEY,
  agent_id     TEXT NOT NULL,
  kind         TEXT NOT NULL,
  payload      JSONB NOT NULL DEFAULT '{}'::jsonb,
  occurred_at  TIMESTAMPTZ NOT NULL,          -- device clock
  received_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_telemetry_agent_received ON telemetry_events (agent_id, received_at DESC);

-- +goose Down
DROP TABLE telemetry_events;
