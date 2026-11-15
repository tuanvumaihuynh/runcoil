-- name: SessionCreate :execrows
INSERT INTO sessions (token_hash, user_id, expires_at)
SELECT @token_hash::bytea, u.id, now() + @max_lifetime::interval
FROM users u
WHERE u.id = @user_id::uuid
  AND u.password_hash = @password_hash::text
  AND u.disabled_at IS NULL
FOR SHARE;

-- name: SessionGetUserAndTouch :one
WITH live AS (
    SELECT s.token_hash, s.user_id, s.last_seen_at
    FROM sessions s
    WHERE s.token_hash = @token_hash::bytea
      AND s.expires_at > now()
      AND s.last_seen_at > now() - @max_inactive::interval
), touched AS (
    UPDATE sessions
    SET last_seen_at = now()
    FROM live
    WHERE sessions.token_hash = live.token_hash
      AND live.last_seen_at <= now() - interval '1 minute'
)
SELECT u.id, u.username, u.email, u.password_hash, u.is_server_admin, u.disabled_at
FROM live
JOIN users u ON u.id = live.user_id
WHERE u.disabled_at IS NULL;

-- name: SessionExistsForUser :one
SELECT EXISTS (
    SELECT 1
    FROM sessions
    WHERE token_hash = @token_hash::bytea
      AND user_id = @user_id::uuid
      AND expires_at > now()
);

-- name: SessionDelete :exec
DELETE FROM sessions
WHERE token_hash = @token_hash::bytea;

-- name: SessionDeleteOthers :exec
DELETE FROM sessions
WHERE user_id = @user_id::uuid
  AND token_hash <> @token_hash::bytea;
