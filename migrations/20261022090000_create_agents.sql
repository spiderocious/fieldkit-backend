-- +goose Up
CREATE TABLE agents (
  id          TEXT PRIMARY KEY,
  phone       TEXT NOT NULL UNIQUE,
  name        TEXT NOT NULL,
  pin_hash    TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION touch_updated_at() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER agents_updated_at BEFORE UPDATE ON agents
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- +goose Down
DROP TABLE agents;
DROP FUNCTION touch_updated_at;
