-- Development-only analytics fixtures for the example owner account.
--
-- Prerequisite: the Clerk-backed "Tejidos Roma Test" business must already
-- exist (it is created through the normal onboarding flow). The stable UUIDs
-- and idempotency keys make this file safe to run more than once.

BEGIN;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM businesses
        WHERE id = 'a42033ec-4b6e-4728-9439-b3b8c1acd71b'
          AND slug = 'tejidos-roma-test'
    ) THEN
        RAISE EXCEPTION 'Tejidos Roma Test is missing; onboard the example owner before applying this seed';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM users
        WHERE id = '41e599ce-efe6-4879-9291-22953234cb01'
    ) THEN
        RAISE EXCEPTION 'The Tejidos Roma Test owner is missing';
    END IF;
END
$$;

INSERT INTO categories (id, business_id, name, slug)
VALUES (
    '71000000-0000-4000-8000-000000000001',
    'a42033ec-4b6e-4728-9439-b3b8c1acd71b',
    'Accesorios tejidos',
    'accesorios-tejidos'
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    slug = EXCLUDED.slug;

INSERT INTO products (
    id, business_id, category_id, name, slug, description, brand, unit,
    product_type, is_handmade, is_public, status
) VALUES
(
    '72000000-0000-4000-8000-000000000001',
    'a42033ec-4b6e-4728-9439-b3b8c1acd71b',
    '71000000-0000-4000-8000-000000000001',
    'Bufanda de telar',
    'bufanda-de-telar-analytics',
    'Bufanda tejida a mano para demostrar la clasificación ABC y el cálculo EOQ.',
    'Tejidos Roma',
    'pieza',
    'stocked_product',
    true,
    false,
    'active'
),
(
    '72000000-0000-4000-8000-000000000002',
    'a42033ec-4b6e-4728-9439-b3b8c1acd71b',
    '71000000-0000-4000-8000-000000000001',
    'Monedero tejido',
    'monedero-tejido-analytics',
    'Monedero tejido a mano con existencias por debajo del punto de reorden.',
    'Tejidos Roma',
    'pieza',
    'stocked_product',
    true,
    false,
    'active'
)
ON CONFLICT (id) DO UPDATE SET
    category_id = EXCLUDED.category_id,
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    brand = EXCLUDED.brand,
    unit = EXCLUDED.unit,
    product_type = EXCLUDED.product_type,
    is_handmade = EXCLUDED.is_handmade,
    is_public = EXCLUDED.is_public,
    status = EXCLUDED.status,
    updated_at = now();

INSERT INTO product_variants (
    id, product_id, business_id, sku, internal_code, name, attributes,
    price, cost, currency, track_inventory, public_stock_status,
    reorder_point, lead_time_days, status, is_public
) VALUES
(
    '73000000-0000-4000-8000-000000000001',
    '72000000-0000-4000-8000-000000000001',
    'a42033ec-4b6e-4728-9439-b3b8c1acd71b',
    'TR-BUFANDA-AZUL',
    'TR-BUFANDA-AZUL',
    'Azul marino',
    '{"color":"azul marino"}',
    500.00,
    210.00,
    'MXN',
    true,
    'available',
    5,
    7,
    'active',
    false
),
(
    '73000000-0000-4000-8000-000000000002',
    '72000000-0000-4000-8000-000000000002',
    'a42033ec-4b6e-4728-9439-b3b8c1acd71b',
    'TR-MONEDERO-TERRACOTA',
    'TR-MONEDERO-TERRACOTA',
    'Terracota',
    '{"color":"terracota"}',
    350.00,
    120.00,
    'MXN',
    true,
    'low_stock',
    6,
    4,
    'active',
    false
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    attributes = EXCLUDED.attributes,
    price = EXCLUDED.price,
    cost = EXCLUDED.cost,
    currency = EXCLUDED.currency,
    track_inventory = EXCLUDED.track_inventory,
    public_stock_status = EXCLUDED.public_stock_status,
    reorder_point = EXCLUDED.reorder_point,
    lead_time_days = EXCLUDED.lead_time_days,
    status = EXCLUDED.status,
    is_public = EXCLUDED.is_public,
    updated_at = now();

-- Apply stock deltas only for movements inserted by this run. This preserves
-- the inventory-ledger invariant and keeps repeated seed runs idempotent.
WITH inserted AS (
    INSERT INTO stock_movements (
        id, business_id, variant_id, location_id, movement_type, quantity,
        unit_cost, reference_type, reference_id, notes, created_by,
        created_at, idempotency_key
    ) VALUES
    (
        '74000000-0000-4000-8000-000000000001',
        'a42033ec-4b6e-4728-9439-b3b8c1acd71b',
        'a84538bb-8dc3-4c54-aa61-76b5ecba784d',
        '4271668e-4073-45d0-9d6d-46dcfa55f59e',
        'IN_PRODUCTION', 30, 140.00, 'seed', 'analytics-sweater-production',
        'Lote inicial para la demostración de analítica.',
        '41e599ce-efe6-4879-9291-22953234cb01', now() - interval '35 days',
        'seed:analytics:sweater:production'
    ),
    (
        '74000000-0000-4000-8000-000000000002',
        'a42033ec-4b6e-4728-9439-b3b8c1acd71b',
        'a84538bb-8dc3-4c54-aa61-76b5ecba784d',
        '4271668e-4073-45d0-9d6d-46dcfa55f59e',
        'OUT_SALE', 18, NULL, 'seed', 'analytics-sweater-sales',
        'Ventas acumuladas para la demostración de analítica.',
        '41e599ce-efe6-4879-9291-22953234cb01', now() - interval '20 days',
        'seed:analytics:sweater:sales'
    ),
    (
        '74000000-0000-4000-8000-000000000003',
        'a42033ec-4b6e-4728-9439-b3b8c1acd71b',
        '73000000-0000-4000-8000-000000000001',
        '4271668e-4073-45d0-9d6d-46dcfa55f59e',
        'IN_PRODUCTION', 15, 210.00, 'seed', 'analytics-scarf-production',
        'Lote inicial para la demostración de analítica.',
        '41e599ce-efe6-4879-9291-22953234cb01', now() - interval '30 days',
        'seed:analytics:scarf:production'
    ),
    (
        '74000000-0000-4000-8000-000000000004',
        'a42033ec-4b6e-4728-9439-b3b8c1acd71b',
        '73000000-0000-4000-8000-000000000001',
        '4271668e-4073-45d0-9d6d-46dcfa55f59e',
        'OUT_SALE', 3, NULL, 'seed', 'analytics-scarf-sales',
        'Ventas acumuladas para la demostración de analítica.',
        '41e599ce-efe6-4879-9291-22953234cb01', now() - interval '12 days',
        'seed:analytics:scarf:sales'
    ),
    (
        '74000000-0000-4000-8000-000000000005',
        'a42033ec-4b6e-4728-9439-b3b8c1acd71b',
        '73000000-0000-4000-8000-000000000002',
        '4271668e-4073-45d0-9d6d-46dcfa55f59e',
        'IN_PRODUCTION', 7, 120.00, 'seed', 'analytics-wallet-production',
        'Lote inicial para la demostración de analítica.',
        '41e599ce-efe6-4879-9291-22953234cb01', now() - interval '25 days',
        'seed:analytics:wallet:production'
    ),
    (
        '74000000-0000-4000-8000-000000000006',
        'a42033ec-4b6e-4728-9439-b3b8c1acd71b',
        '73000000-0000-4000-8000-000000000002',
        '4271668e-4073-45d0-9d6d-46dcfa55f59e',
        'OUT_SALE', 2, NULL, 'seed', 'analytics-wallet-sales',
        'Ventas acumuladas para la demostración de analítica.',
        '41e599ce-efe6-4879-9291-22953234cb01', now() - interval '6 days',
        'seed:analytics:wallet:sales'
    )
    ON CONFLICT (business_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
    RETURNING business_id, variant_id, location_id, movement_type, quantity
), deltas AS (
    SELECT
        business_id,
        variant_id,
        location_id,
        SUM(
            CASE
                WHEN movement_type IN ('IN_PURCHASE', 'IN_PRODUCTION', 'IN_ADJUSTMENT') THEN quantity
                ELSE -quantity
            END
        ) AS quantity_on_hand
    FROM inserted
    GROUP BY business_id, variant_id, location_id
)
INSERT INTO stock_levels (
    business_id, variant_id, location_id, quantity_on_hand, quantity_reserved
)
SELECT business_id, variant_id, location_id, quantity_on_hand, 0
FROM deltas
ON CONFLICT (business_id, variant_id, location_id) DO UPDATE SET
    quantity_on_hand = stock_levels.quantity_on_hand + EXCLUDED.quantity_on_hand,
    updated_at = now();

REFRESH MATERIALIZED VIEW product_sales_summary;

COMMIT;
