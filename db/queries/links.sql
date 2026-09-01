-- name: ListLinks :many
SELECT id, original_url, short_name, created_at
FROM links
ORDER BY id;

-- name: GetLink :one
SELECT id, original_url, short_name, created_at
FROM links
WHERE id = $1;

-- name: GetLinkByShortName :one
SELECT id, original_url, short_name, created_at
FROM links
WHERE short_name = $1;

-- name: CreateLink :one
INSERT INTO links (original_url, short_name)
VALUES ($1, $2)
RETURNING id, original_url, short_name, created_at;

-- name: UpdateLink :one
UPDATE links
SET original_url = $2, short_name = $3
WHERE id = $1
RETURNING id, original_url, short_name, created_at;

-- name: DeleteLink :execrows
DELETE FROM links
WHERE id = $1;

-- name: ListLinksRange :many
SELECT id, original_url, short_name, created_at
FROM links
ORDER BY id
LIMIT $1 OFFSET $2;

-- name: CountLinks :one
SELECT count(*) FROM links;
