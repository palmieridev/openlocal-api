-- name: EnsureSubscription :exec
INSERT INTO business_subscriptions(business_id) VALUES($1) ON CONFLICT DO NOTHING;

-- name: LockSubscription :one
SELECT * FROM business_subscriptions WHERE business_id=$1 FOR UPDATE;

-- name: GetSubscription :one
SELECT * FROM business_subscriptions WHERE business_id=$1;

-- name: GetSubscriptionByCustomer :one
SELECT * FROM business_subscriptions WHERE stripe_customer_id=$1;

-- name: UpdateSubscription :one
UPDATE business_subscriptions SET tier=$2,status=$3,modules=$4,stripe_customer_id=$5,
stripe_subscription_id=$6,period_start=$7,period_end=$8,grace_until=$9,
cancel_at_period_end=$10,scheduled_change=$11,updated_at=now() WHERE business_id=$1 RETURNING *;

-- name: ListBillingCustomers :many
SELECT stripe_customer_id FROM business_subscriptions WHERE stripe_customer_id IS NOT NULL ORDER BY business_id LIMIT $1 OFFSET $2;

-- name: GetModule :one
SELECT * FROM business_modules WHERE business_id=$1 AND module=$2;

-- name: UpsertModule :one
INSERT INTO business_modules(business_id,module,enabled,settings) VALUES($1,$2,$3,$4)
ON CONFLICT(business_id,module) DO UPDATE SET enabled=EXCLUDED.enabled,settings=EXCLUDED.settings RETURNING *;

-- name: ModuleUsage :one
SELECT count(*)::int FROM module_usage_events WHERE business_id=$1 AND module=$2 AND period_start=$3;

-- name: ConsumeModuleUsage :exec
INSERT INTO module_usage_events(business_id,module,record_id,period_start) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING;

-- name: UpsertModuleCustomer :one
INSERT INTO module_customers(business_id,phone,name) VALUES($1,$2,$3)
ON CONFLICT(business_id,phone) DO UPDATE SET name=EXCLUDED.name RETURNING *;

-- name: ModuleBusiness :one
SELECT * FROM businesses WHERE id=$1;

-- name: ModulePublicBusiness :one
SELECT * FROM businesses WHERE slug=$1 AND status='active';

-- name: GetRequestAccess :one
SELECT * FROM request_access WHERE public_id=$1 AND token_hash=$2 AND expires_at>now();

-- name: SaveRequestAccess :exec
INSERT INTO request_access(public_id,business_id,module,token_hash,expires_at) VALUES($1,$2,$3,$4,$5);

-- name: ExtendRequestAccess :exec
UPDATE request_access SET expires_at=$2 WHERE public_id=$1;

-- name: AddRequestHistory :exec
INSERT INTO request_history(business_id,appointment_id,preorder_id,from_status,to_status,actor) VALUES($1,$2,$3,$4,$5,$6);

-- name: RequestHistory :many
SELECT from_status,to_status,actor,at FROM request_history WHERE business_id=$1 AND (appointment_id=$2 OR preorder_id=$2) ORDER BY id;

-- name: GetRequestReplay :one
SELECT * FROM request_idempotency WHERE business_id=$1 AND scope=$2 AND key=$3 AND expires_at>now();

-- name: SaveRequestReplay :exec
INSERT INTO request_idempotency(business_id,scope,key,digest,response,status_code) VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(business_id,scope,key) DO UPDATE SET digest=EXCLUDED.digest,response=EXCLUDED.response,status_code=EXCLUDED.status_code,expires_at=now()+interval '24 hours';

-- name: CleanRequestReplay :exec
DELETE FROM request_idempotency WHERE expires_at<now();

-- name: CleanRequestAccess :exec
DELETE FROM request_access WHERE expires_at<now();

-- name: SaveBillingEvent :execrows
INSERT INTO billing_events(id,customer_id) VALUES($1,$2) ON CONFLICT DO NOTHING;

-- name: ProcessBillingEvents :exec
UPDATE billing_events SET status='processed',processed_at=now() WHERE customer_id=$1 AND status='pending';

-- name: GetBillingOperation :one
SELECT * FROM billing_operations WHERE business_id=$1 AND key=$2;

-- name: SaveBillingOperation :exec
INSERT INTO billing_operations(business_id,key,digest,response) VALUES($1,$2,$3,$4)
ON CONFLICT(business_id,key) DO UPDATE SET response=EXCLUDED.response;

-- name: EnqueueJob :one
SELECT openlocal_enqueue(sqlc.arg(payload)::jsonb,sqlc.arg(delay_seconds)::int)::bigint AS id;

-- name: ReadJobs :one
SELECT openlocal_read_jobs()::jsonb AS jobs;

-- name: AckJob :one
SELECT openlocal_ack_job(sqlc.arg(message_id)::bigint)::boolean AS acknowledged;

-- name: RetryJob :exec
SELECT openlocal_retry_job(sqlc.arg(message_id)::bigint,sqlc.arg(seconds)::int);

-- name: FailJob :exec
SELECT openlocal_fail_job(sqlc.arg(message_id)::bigint,sqlc.arg(payload)::jsonb);

-- name: CleanJobArchive :exec
SELECT openlocal_clean_job_archive();
