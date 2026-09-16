-- name: ListServices :many
SELECT * FROM services WHERE business_id=$1 ORDER BY name,id;
-- name: GetService :one
SELECT * FROM services WHERE business_id=$1 AND id=$2;
-- name: SaveService :one
INSERT INTO services(id,business_id,name,description,duration_minutes,price,is_public,active,buffer_minutes)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,duration_minutes=EXCLUDED.duration_minutes,
price=EXCLUDED.price,is_public=EXCLUDED.is_public,active=EXCLUDED.active,buffer_minutes=EXCLUDED.buffer_minutes
WHERE services.business_id=EXCLUDED.business_id RETURNING *;
-- name: ListAppointmentResources :many
SELECT * FROM appointment_resources WHERE business_id=$1 ORDER BY name,id;
-- name: GetAppointmentResource :one
SELECT * FROM appointment_resources WHERE business_id=$1 AND id=$2;
-- name: SaveAppointmentResource :one
INSERT INTO appointment_resources(id,business_id,name,active,service_ids,hours,closed_dates) VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,active=EXCLUDED.active,service_ids=EXCLUDED.service_ids,hours=EXCLUDED.hours,closed_dates=EXCLUDED.closed_dates
WHERE appointment_resources.business_id=EXCLUDED.business_id RETURNING *;
-- name: ListAppointments :many
SELECT * FROM appointments WHERE business_id=sqlc.arg(business_id)
AND (sqlc.arg(status)::text='' OR status=sqlc.arg(status))
AND (sqlc.narg(resource_id)::uuid IS NULL OR resource_id=sqlc.narg(resource_id))
AND (sqlc.narg(customer_id)::uuid IS NULL OR customer_id=sqlc.narg(customer_id))
AND (sqlc.narg(from_at)::timestamptz IS NULL OR starts_at>=sqlc.narg(from_at))
AND (sqlc.narg(to_at)::timestamptz IS NULL OR starts_at<sqlc.narg(to_at))
ORDER BY starts_at DESC,id LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);
-- name: CountAppointments :one
SELECT count(*)::int FROM appointments WHERE business_id=sqlc.arg(business_id)
AND (sqlc.arg(status)::text='' OR status=sqlc.arg(status))
AND (sqlc.narg(resource_id)::uuid IS NULL OR resource_id=sqlc.narg(resource_id))
AND (sqlc.narg(customer_id)::uuid IS NULL OR customer_id=sqlc.narg(customer_id))
AND (sqlc.narg(from_at)::timestamptz IS NULL OR starts_at>=sqlc.narg(from_at))
AND (sqlc.narg(to_at)::timestamptz IS NULL OR starts_at<sqlc.narg(to_at));
-- name: GetAppointment :one
SELECT * FROM appointments WHERE business_id=$1 AND id=$2;
-- name: AppointmentOverlaps :one
SELECT EXISTS(SELECT 1 FROM appointments WHERE business_id=$1 AND resource_id=$2 AND id<>$3
AND status IN ('confirmed','completed','no_show')
AND starts_at < sqlc.arg(occupied_end)::timestamptz
AND ends_at + buffer_minutes * interval '1 minute' > sqlc.arg(starts_at)::timestamptz)::boolean AS overlaps;
-- name: CreateAppointment :one
INSERT INTO appointments(id,business_id,customer_id,service_id,resource_id,customer_name,customer_phone,notes,service_name,duration_minutes,buffer_minutes,price,starts_at,ends_at,timezone,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING *;
-- name: UpdateAppointment :one
UPDATE appointments SET resource_id=$3,notes=$4,starts_at=$5,ends_at=$6,status=$7,expires_at=$8,
confirmed_at=$9,version=version+1,updated_at=now() WHERE business_id=$1 AND id=$2 RETURNING *;
-- name: PendingAppointmentsForPhone :one
SELECT count(*)::int FROM appointments WHERE business_id=$1 AND status='requested' AND expires_at>now()
AND (sqlc.arg(phone)::text='' OR customer_phone=sqlc.arg(phone));
-- name: DueAppointments :many
SELECT id,business_id FROM appointments WHERE status='requested' AND expires_at<=now() ORDER BY expires_at LIMIT 100;
