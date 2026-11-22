-- name: TryAdvisoryLock :one
SELECT pg_try_advisory_lock(sqlc.arg(lock_key)::bigint) AS acquired;

-- name: AdvisoryUnlock :one
SELECT pg_advisory_unlock(sqlc.arg(lock_key)::bigint) AS released;

-- name: UpsertWorkerRunStart :exec
INSERT INTO worker_runs (job_name, last_started_at, last_status, updated_at)
VALUES (sqlc.arg(job_name), NOW(), 'running', NOW())
ON CONFLICT (job_name) DO UPDATE
SET last_started_at = NOW(),
    last_status = 'running',
    updated_at = NOW();

-- name: UpsertWorkerRunFinish :exec
UPDATE worker_runs
SET last_finished_at = NOW(),
    last_status = sqlc.arg(last_status),
    last_summary = sqlc.arg(last_summary),
    last_error = sqlc.arg(last_error),
    run_count = run_count + 1,
    updated_at = NOW()
WHERE job_name = sqlc.arg(job_name);

-- name: ListWorkerRuns :many
SELECT job_name, last_started_at, last_finished_at, last_status, last_summary, last_error, run_count, updated_at
FROM worker_runs
ORDER BY job_name;
