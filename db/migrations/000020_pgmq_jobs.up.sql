CREATE EXTENSION IF NOT EXISTS pgmq;
SELECT pgmq.create('openlocal_jobs');
SELECT pgmq.create('openlocal_failed');

-- Wrappers give sqlc stable scalar types while leaving queue storage to PGMQ.
CREATE FUNCTION openlocal_enqueue(payload jsonb, delay_seconds integer) RETURNS bigint
LANGUAGE sql AS $$ SELECT pgmq.send('openlocal_jobs', payload, delay_seconds); $$;
CREATE FUNCTION openlocal_read_jobs() RETURNS jsonb
LANGUAGE sql AS $$ SELECT COALESCE(jsonb_agg(to_jsonb(j)), '[]'::jsonb) FROM pgmq.read('openlocal_jobs', 60, 10) j; $$;
CREATE FUNCTION openlocal_ack_job(message_id bigint) RETURNS boolean
LANGUAGE sql AS $$ SELECT pgmq.archive('openlocal_jobs', message_id); $$;
CREATE FUNCTION openlocal_retry_job(message_id bigint, seconds integer) RETURNS void
LANGUAGE sql AS $$ SELECT pgmq.set_vt('openlocal_jobs', message_id, seconds); $$;
CREATE FUNCTION openlocal_fail_job(message_id bigint, payload jsonb) RETURNS void
LANGUAGE plpgsql AS $$ BEGIN
  PERFORM pgmq.send('openlocal_failed', payload);
  PERFORM pgmq.delete('openlocal_jobs', message_id);
END $$;
CREATE FUNCTION openlocal_clean_job_archive() RETURNS void
LANGUAGE sql AS $$ DELETE FROM pgmq.a_openlocal_jobs WHERE archived_at < now()-interval '7 days'; $$;
