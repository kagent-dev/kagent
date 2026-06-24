CREATE TABLE IF NOT EXISTS session_share (
    id         BIGSERIAL   PRIMARY KEY,
    token      TEXT        UNIQUE NOT NULL,
    session_id TEXT        NOT NULL,
    user_id    TEXT        NOT NULL,
    read_only  BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP   NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_session_share_session_id ON session_share (session_id);

CREATE TABLE IF NOT EXISTS session_share_access (
    user_id     TEXT    NOT NULL,
    share_id    BIGINT  NOT NULL REFERENCES session_share(id) ON DELETE CASCADE,
    accessed_at TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, share_id)
);
