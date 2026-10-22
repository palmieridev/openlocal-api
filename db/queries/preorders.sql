-- name: PreorderVariants :many
SELECT pv.id,pv.product_id,p.name AS product_name,pv.name,p.unit,pv.price,pv.currency
FROM product_variants pv JOIN products p ON p.id=pv.product_id AND p.business_id=pv.business_id
WHERE pv.business_id=$1 AND pv.status='active' AND pv.is_public AND p.status='active' AND p.is_public
ORDER BY p.name,pv.name,pv.id;
-- name: UpsertPickupWindow :one
INSERT INTO preorder_pickup_windows(business_id,starts_at,ends_at) VALUES($1,$2,$3)
ON CONFLICT(business_id,starts_at,ends_at) DO UPDATE SET ends_at=EXCLUDED.ends_at RETURNING *;
-- name: GetPickupWindow :one
SELECT * FROM preorder_pickup_windows WHERE business_id=$1 AND id=$2;
-- name: PickupWindowUsage :one
SELECT count(*)::int FROM preorders WHERE business_id=$1 AND pickup_window_id=$2 AND id<>$3
AND status IN ('confirmed','preparing','ready','collected','uncollected');
-- name: CreatePreorder :one
INSERT INTO preorders(id,business_id,customer_id,pickup_window_id,customer_name,customer_phone,notes,starts_at,ends_at,timezone,estimated_total,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;
-- name: CreatePreorderItem :one
INSERT INTO preorder_items(business_id,preorder_id,variant_id,name,unit,quantity,unit_price,total)
VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *;
-- name: GetPreorder :one
SELECT * FROM preorders WHERE business_id=$1 AND id=$2;
-- name: PreorderItems :many
SELECT * FROM preorder_items WHERE business_id=$1 AND preorder_id=$2 ORDER BY id;
-- name: UpdatePreorderItem :execrows
UPDATE preorder_items SET final_quantity=$4 WHERE business_id=$1 AND preorder_id=$2 AND id=$3;
-- name: UpdatePreorder :one
UPDATE preorders SET pickup_window_id=$3,notes=$4,starts_at=$5,ends_at=$6,status=$7,expires_at=$8,
confirmed_at=$9,final_total=$10,version=version+1,updated_at=now() WHERE business_id=$1 AND id=$2 RETURNING *;
-- name: ListPreorders :many
SELECT * FROM preorders WHERE business_id=sqlc.arg(business_id)
AND (sqlc.arg(status)::text='' OR status=sqlc.arg(status))
AND (sqlc.narg(customer_id)::uuid IS NULL OR customer_id=sqlc.narg(customer_id))
AND (sqlc.narg(from_at)::timestamptz IS NULL OR starts_at>=sqlc.narg(from_at))
AND (sqlc.narg(to_at)::timestamptz IS NULL OR starts_at<sqlc.narg(to_at))
ORDER BY starts_at DESC,id LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);
-- name: CountPreorders :one
SELECT count(*)::int FROM preorders WHERE business_id=sqlc.arg(business_id)
AND (sqlc.arg(status)::text='' OR status=sqlc.arg(status))
AND (sqlc.narg(customer_id)::uuid IS NULL OR customer_id=sqlc.narg(customer_id))
AND (sqlc.narg(from_at)::timestamptz IS NULL OR starts_at>=sqlc.narg(from_at))
AND (sqlc.narg(to_at)::timestamptz IS NULL OR starts_at<sqlc.narg(to_at));
-- name: PendingPreordersForPhone :one
SELECT count(*)::int FROM preorders WHERE business_id=$1 AND status='requested' AND expires_at>now()
AND (sqlc.arg(phone)::text='' OR customer_phone=sqlc.arg(phone));
-- name: DuePreorders :many
SELECT id,business_id FROM preorders WHERE status='requested' AND expires_at<=now() ORDER BY expires_at LIMIT 100;
-- name: PreorderSummaryItems :many
SELECT i.variant_id,i.name,i.unit,sum(COALESCE(i.final_quantity,i.quantity))::numeric(12,3) AS quantity
FROM preorder_items i JOIN preorders p ON p.business_id=i.business_id AND p.id=i.preorder_id
WHERE p.business_id=$1 AND p.starts_at>=$2 AND p.starts_at<$3 AND p.status IN ('confirmed','preparing','ready','collected')
GROUP BY i.variant_id,i.name,i.unit ORDER BY i.name;
-- name: PreorderSummaryTotals :one
SELECT count(*)::int AS orders,COALESCE(sum(COALESCE(final_total,estimated_total)),0)::numeric(12,2) AS total_estimated
FROM preorders WHERE business_id=$1 AND starts_at>=$2 AND starts_at<$3 AND status IN ('confirmed','preparing','ready','collected');
