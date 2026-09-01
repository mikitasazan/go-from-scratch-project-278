-- +goose Up
CREATE TABLE links (
    id           BIGSERIAL PRIMARY KEY,
    original_url TEXT NOT NULL,
    short_name   TEXT NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE links;
