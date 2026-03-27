-- name: InsertMemory :one
INSERT INTO memory (agent_name, user_id, content, embedding, metadata, created_at, expires_at, access_count)
VALUES ($1, $2, $3, $4, $5, NOW(), $6, $7)
RETURNING id;

-- name: SearchAgentMemory :many
SELECT *, COALESCE(1 - (embedding <=> $1), 0) AS score
FROM memory
WHERE agent_name = $2 AND user_id = $3
ORDER BY embedding <=> $1 ASC
LIMIT $4;

-- name: IncrementMemoryAccessCount :exec
UPDATE memory SET access_count = access_count + 1
WHERE id = ANY($1::text[]);

-- name: ListAgentMemories :many
SELECT * FROM memory
WHERE (agent_name = $1 OR agent_name = $2) AND user_id = $3
ORDER BY access_count DESC;

-- name: DeleteAgentMemory :exec
DELETE FROM memory WHERE agent_name = $1 AND user_id = $2;

-- name: ExtendMemoryTTL :exec
UPDATE memory
SET expires_at = NOW() + INTERVAL '15 days', access_count = 0
WHERE expires_at < NOW() AND access_count >= 10;

-- name: DeleteExpiredMemories :exec
DELETE FROM memory
WHERE expires_at < NOW() AND access_count < 10;
