-- name: InsertJob :one
INSERT INTO jobs.jobs (tenant_id, queue, kind, payload, payload_version, status, priority, run_at, attempt, max_attempts, idempotency_key, correlation_id)
VALUES (sqlc.narg(tenant_id), @queue, @kind, @payload, @payload_version, 'queued', @priority, @run_at, 0, @max_attempts, @idempotency_key, sqlc.narg(correlation_id))
ON CONFLICT (tenant_id, kind, idempotency_key) DO NOTHING
RETURNING id;

-- name: FindJobByKey :one
SELECT id FROM jobs.jobs WHERE tenant_id IS NOT DISTINCT FROM sqlc.narg(tenant_id) AND kind = @kind AND idempotency_key = @idempotency_key;

-- name: GetJob :one
SELECT id, tenant_id, queue, kind, status, attempt, max_attempts, progress, cancel_requested_at, last_error, run_at, finished_at, created_at
FROM jobs.jobs WHERE id = @id;

-- name: ClaimJob :one
-- Fair sharing: a tenant at its concurrency limit is passed over, so a large job cannot starve others.
UPDATE jobs.jobs SET status = 'running', attempt = attempt + 1, locked_by = @worker_id, lock_expires_at = @lock_expires_at
WHERE id = (
  SELECT j.id FROM jobs.jobs j
  WHERE j.queue = @queue AND j.status IN ('queued', 'retry_scheduled') AND j.run_at <= now()
    AND (j.tenant_id IS NULL OR (
      SELECT count(*) FROM jobs.jobs r WHERE r.tenant_id = j.tenant_id AND r.queue = j.queue AND r.status = 'running'
    ) < @tenant_limit::bigint)
  ORDER BY j.priority DESC, j.run_at
  LIMIT 1
  FOR UPDATE SKIP LOCKED
)
RETURNING id, tenant_id, queue, kind, payload, payload_version, attempt, max_attempts, correlation_id, cancel_requested_at;

-- name: Heartbeat :one
UPDATE jobs.jobs SET lock_expires_at = @lock_expires_at
WHERE id = @id AND locked_by = @worker_id AND status = 'running'
RETURNING cancel_requested_at;

-- name: SetProgress :exec
UPDATE jobs.jobs SET progress = @progress WHERE id = @id AND status = 'running';

-- name: FinishJob :execrows
-- The worker check means a worker whose lock expired cannot overwrite the newer attempt.
UPDATE jobs.jobs SET status = @status, finished_at = now(), locked_by = NULL, lock_expires_at = NULL, last_error = sqlc.narg(last_error)
WHERE id = @id AND locked_by = @worker_id AND status = 'running';

-- name: RescheduleJob :execrows
UPDATE jobs.jobs SET status = 'retry_scheduled', run_at = @run_at, locked_by = NULL, lock_expires_at = NULL, last_error = sqlc.narg(last_error),
    attempt = attempt - @refund_attempt::int
WHERE id = @id AND locked_by = @worker_id AND status = 'running';

-- name: InsertAttempt :exec
INSERT INTO jobs.job_attempts (job_id, attempt, worker_id, started_at, finished_at, outcome, error)
VALUES (@job_id, @attempt, @worker_id, @started_at, now(), @outcome, sqlc.narg(error))
ON CONFLICT (job_id, attempt) DO UPDATE SET finished_at = now(), outcome = EXCLUDED.outcome, error = EXCLUDED.error;

-- name: ListExpiredLocks :many
SELECT id, tenant_id, kind, payload, attempt, max_attempts FROM jobs.jobs
WHERE status = 'running' AND lock_expires_at < now()
LIMIT 100 FOR UPDATE SKIP LOCKED;

-- name: ReleaseExpired :exec
UPDATE jobs.jobs SET status = 'retry_scheduled', run_at = now(), locked_by = NULL, lock_expires_at = NULL, last_error = 'worker lost its lock' WHERE id = @id;

-- name: MarkDead :exec
UPDATE jobs.jobs SET status = 'dead', finished_at = now(), locked_by = NULL, lock_expires_at = NULL, last_error = sqlc.narg(last_error) WHERE id = @id;

-- name: InsertDeadLetter :exec
INSERT INTO jobs.dead_letters (tenant_id, origin, origin_id, kind, payload, error)
VALUES (sqlc.narg(tenant_id), @origin, @origin_id, @kind, @payload, @error);

-- name: RequestCancel :one
-- A job that has not started is cancelled now. A running job sees the flag at its next heartbeat.
UPDATE jobs.jobs SET cancel_requested_at = now(),
    status = CASE WHEN status IN ('queued', 'retry_scheduled') THEN 'cancelled' ELSE status END,
    finished_at = CASE WHEN status IN ('queued', 'retry_scheduled') THEN now() ELSE finished_at END
WHERE id = @id AND status IN ('queued', 'retry_scheduled', 'running')
RETURNING status;

-- name: ListDueSchedules :many
SELECT id, tenant_id, key, cron, kind, payload FROM jobs.job_schedules
WHERE enabled AND next_run_at <= now() FOR UPDATE SKIP LOCKED;

-- name: AdvanceSchedule :exec
UPDATE jobs.job_schedules SET next_run_at = @next_run_at, last_run_at = now() WHERE id = @id;

-- name: UpsertSchedule :exec
INSERT INTO jobs.job_schedules (tenant_id, key, cron, timezone, kind, payload, next_run_at, enabled)
VALUES (NULL, @key, @cron, 'UTC', @kind, '{}', now(), true)
ON CONFLICT (tenant_id, key) DO UPDATE SET cron = EXCLUDED.cron, kind = EXCLUDED.kind;

-- name: PurgeFinishedJobs :exec
DELETE FROM jobs.jobs WHERE status IN ('succeeded', 'failed', 'cancelled') AND finished_at < @before;

-- name: QueueStats :many
SELECT queue, status, count(*)::bigint AS jobs, COALESCE(EXTRACT(EPOCH FROM now() - min(run_at)), 0)::bigint AS oldest_seconds
FROM jobs.jobs WHERE status IN ('queued', 'retry_scheduled', 'running') GROUP BY queue, status ORDER BY queue, status;

-- name: ListDeadLetters :many
SELECT id, tenant_id, origin, origin_id, kind, error, resolution, created_at FROM jobs.dead_letters
WHERE (sqlc.arg(include_resolved)::boolean OR resolution IS NULL) ORDER BY created_at DESC LIMIT 200;

-- name: GetDeadLetter :one
SELECT id, tenant_id, origin, origin_id, kind, resolution FROM jobs.dead_letters WHERE id = @id FOR UPDATE;

-- name: ResolveDeadLetter :exec
UPDATE jobs.dead_letters SET resolution = @resolution, resolved_by = @resolved_by, resolved_at = now() WHERE id = @id;

-- name: RequeueJob :exec
UPDATE jobs.jobs SET status = 'queued', attempt = 0, run_at = now(), finished_at = NULL, last_error = NULL WHERE id = @id AND status = 'dead';

-- name: RequeueDelivery :exec
UPDATE jobs.event_deliveries SET status = 'pending', attempts = 0, next_attempt_at = now(), last_error = NULL WHERE id = @id AND status = 'dead';

-- name: AppendEvent :exec
INSERT INTO jobs.outbox_events (event_id, event_type, schema_version, tenant_id, source_module, aggregate_type, aggregate_id, occurred_at, correlation_id, causation_id, payload)
VALUES (@event_id, @event_type, @schema_version, sqlc.narg(tenant_id), @source_module, @aggregate_type, @aggregate_id, @occurred_at, @correlation_id, sqlc.narg(causation_id), @payload);

-- name: ClaimUnpublished :many
SELECT event_id, event_type, tenant_id, occurred_at, sequence FROM jobs.outbox_events
WHERE published_at IS NULL ORDER BY sequence LIMIT @batch FOR UPDATE SKIP LOCKED;

-- name: MarkPublished :exec
UPDATE jobs.outbox_events SET published_at = now() WHERE event_id = @event_id AND occurred_at = @occurred_at;

-- name: InsertDelivery :exec
INSERT INTO jobs.event_deliveries (tenant_id, event_id, consumer, status, attempts, next_attempt_at)
VALUES (sqlc.narg(tenant_id), @event_id, @consumer, 'pending', 0, now())
ON CONFLICT (event_id, consumer) DO NOTHING;

-- name: ClaimDeliveries :many
-- An event waits while an earlier event for the same aggregate is unfinished for this consumer.
-- A dead delivery does not count, so one poison event cannot block its aggregate forever.
SELECT d.id, d.consumer, d.attempts, e.event_id, e.event_type, e.schema_version, e.tenant_id, e.source_module,
       e.aggregate_type, e.aggregate_id, e.occurred_at, e.correlation_id, e.causation_id, e.payload
FROM jobs.event_deliveries d
JOIN jobs.outbox_events e ON e.event_id = d.event_id
WHERE d.status IN ('pending', 'failed') AND COALESCE(d.next_attempt_at, now()) <= now()
  AND NOT EXISTS (
    SELECT 1 FROM jobs.event_deliveries d2 JOIN jobs.outbox_events e2 ON e2.event_id = d2.event_id
    WHERE d2.consumer = d.consumer AND d2.status IN ('pending', 'processing', 'failed')
      AND e2.aggregate_type = e.aggregate_type AND e2.aggregate_id = e.aggregate_id AND e2.sequence < e.sequence
  )
ORDER BY e.sequence
LIMIT @batch
FOR UPDATE OF d SKIP LOCKED;

-- name: MarkDeliveryProcessing :exec
UPDATE jobs.event_deliveries SET status = 'processing', attempts = attempts + 1 WHERE id = @id;

-- name: CompleteDelivery :execrows
-- Runs in the consumer's own transaction under tenant context, so the effect and this mark commit together.
UPDATE jobs.event_deliveries SET status = 'processed', processed_at = now(), last_error = NULL
WHERE id = @id AND status <> 'processed';

-- name: FailDelivery :exec
UPDATE jobs.event_deliveries SET status = @status, next_attempt_at = sqlc.narg(next_attempt_at), last_error = @last_error WHERE id = @id;

-- name: ReleaseStuckDeliveries :exec
UPDATE jobs.event_deliveries SET status = 'failed', next_attempt_at = now(), last_error = 'worker lost the delivery'
WHERE status = 'processing' AND updated_at < @before;

-- name: ReplayDeliveries :execrows
UPDATE jobs.event_deliveries d SET status = 'pending', next_attempt_at = now(), attempts = 0, processed_at = NULL
FROM jobs.outbox_events e
WHERE e.event_id = d.event_id AND d.consumer = @consumer AND e.event_type = @event_type AND e.occurred_at >= @since;

-- name: EventStats :one
SELECT COALESCE((SELECT EXTRACT(EPOCH FROM now() - min(occurred_at)) FROM jobs.outbox_events WHERE published_at IS NULL), 0)::bigint AS oldest_unpublished_seconds,
       (SELECT count(*) FROM jobs.event_deliveries WHERE status IN ('pending', 'processing', 'failed'))::bigint AS pending_deliveries,
       (SELECT count(*) FROM jobs.event_deliveries WHERE status = 'dead')::bigint AS dead_deliveries;
