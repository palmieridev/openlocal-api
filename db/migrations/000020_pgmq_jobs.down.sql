-- Refuse to silently discard pending work during a rollback.
DO $$ BEGIN
  IF EXISTS(SELECT 1 FROM pgmq.q_openlocal_jobs) OR EXISTS(SELECT 1 FROM pgmq.q_openlocal_failed) THEN
    RAISE EXCEPTION 'Drain and export operational queues before rollback';
  END IF;
END $$;
DROP FUNCTION openlocal_clean_job_archive(),openlocal_fail_job(bigint,jsonb),openlocal_retry_job(bigint,integer),openlocal_ack_job(bigint),openlocal_read_jobs(),openlocal_enqueue(jsonb,integer);
SELECT pgmq.drop_queue('openlocal_jobs');
SELECT pgmq.drop_queue('openlocal_failed');
-- PGMQ can also serve other applications; leave the extension installed.
