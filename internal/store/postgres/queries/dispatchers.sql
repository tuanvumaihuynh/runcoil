-- name: DispatcherBeat :exec
INSERT INTO dispatchers (id) VALUES (@id::uuid)
ON CONFLICT (id) DO UPDATE SET heartbeat_at = now();
