-- Task get/delete/create had no owner check at all: any user could read,
-- overwrite, or delete any other user's task by id. user_id lets queries
-- scope by owner the same way session and event already do.
ALTER TABLE task ADD COLUMN IF NOT EXISTS user_id TEXT;

-- session.id is not globally unique (session's key is (id, user_id)), so only
-- backfill from a session id that maps to exactly one user across its whole
-- history, deleted sessions included. Filtering deleted sessions first could
-- misassign: if Alice's s1 is deleted and Bob's s1 is active, Bob would look
-- like the sole owner of tasks Alice created. Anything ambiguous is left NULL,
-- which the new owner checks then hide from everyone rather than guessing wrong.
WITH unique_session AS (
    SELECT id, MIN(user_id) AS user_id
    FROM session
    GROUP BY id
    HAVING COUNT(DISTINCT user_id) = 1
)
UPDATE task
SET user_id = unique_session.user_id
FROM unique_session
WHERE unique_session.id = task.session_id AND task.user_id IS NULL;
