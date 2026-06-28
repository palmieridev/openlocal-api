BEGIN;

INSERT INTO businesses (
    id, name, slug, description, business_type, phone, whatsapp, email, website,
    logo_url, cover_image_url, status, address, neighborhood, city, state, country,
    postal_code, latitude, longitude, pickup_available, delivery_available
) VALUES
(
    '11111111-1111-4111-8111-111111111111',
    'Mercado Verde Roma',
    'mercado-verde-roma',
    'Fresh produce, pantry staples, and prepared foods from neighborhood growers.',
    'grocery',
    '+525555010101',
    '+525555010101',
    'hola@mercadoverde.example',
    'https://openlocal.example/mercado-verde-roma',
    'https://images.unsplash.com/photo-1542838132-92c53300491e?auto=format&fit=crop&w=320&q=80',
    'https://images.unsplash.com/photo-1488459716781-31db52582fe9?auto=format&fit=crop&w=1400&q=80',
    'active',
    'Calle Colima 145',
    'Roma Norte',
    'CDMX',
    'CDMX',
    'MX',
    '06700',
    19.419400,
    -99.159100,
    true,
    true
),
(
    '22222222-2222-4222-8222-222222222222',
    'Casa Pan Local',
    'casa-pan-local',
    'Small-batch sourdough, pastries, and pantry goods baked every morning.',
    'bakery',
    '+525555020202',
    '+525555020202',
    'pedidos@casapan.example',
    'https://openlocal.example/casa-pan-local',
    'https://images.unsplash.com/photo-1509440159596-0249088772ff?auto=format&fit=crop&w=320&q=80',
    'https://images.unsplash.com/photo-1517433670267-08bbd4be890f?auto=format&fit=crop&w=1400&q=80',
    'active',
    'Av. Amsterdam 210',
    'Condesa',
    'CDMX',
    'CDMX',
    'MX',
    '06100',
    19.411700,
    -99.171800,
    true,
    false
),
(
    '33333333-3333-4333-8333-333333333333',
    'Taller Botanico',
    'taller-botanico',
    'Plants, ceramic pots, soil mixes, and care kits for urban homes.',
    'garden',
    '+525555030303',
    '+525555030303',
    'contacto@tallerbotanico.example',
    'https://openlocal.example/taller-botanico',
    'https://images.unsplash.com/photo-1485955900006-10f4d324d411?auto=format&fit=crop&w=320&q=80',
    'https://images.unsplash.com/photo-1459156212016-c812468e2115?auto=format&fit=crop&w=1400&q=80',
    'active',
    'Sinaloa 88',
    'Roma Norte',
    'CDMX',
    'CDMX',
    'MX',
    '06700',
    19.415700,
    -99.164900,
    true,
    true
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    slug = EXCLUDED.slug,
    description = EXCLUDED.description,
    business_type = EXCLUDED.business_type,
    phone = EXCLUDED.phone,
    whatsapp = EXCLUDED.whatsapp,
    email = EXCLUDED.email,
    website = EXCLUDED.website,
    logo_url = EXCLUDED.logo_url,
    cover_image_url = EXCLUDED.cover_image_url,
    status = EXCLUDED.status,
    address = EXCLUDED.address,
    neighborhood = EXCLUDED.neighborhood,
    city = EXCLUDED.city,
    state = EXCLUDED.state,
    country = EXCLUDED.country,
    postal_code = EXCLUDED.postal_code,
    latitude = EXCLUDED.latitude,
    longitude = EXCLUDED.longitude,
    pickup_available = EXCLUDED.pickup_available,
    delivery_available = EXCLUDED.delivery_available,
    updated_at = now();

INSERT INTO inventory_locations (id, business_id, name, is_default) VALUES
('aaaaaaaa-1111-4111-8111-111111111111', '11111111-1111-4111-8111-111111111111', 'Main Store', true),
('aaaaaaaa-2222-4222-8222-222222222222', '22222222-2222-4222-8222-222222222222', 'Bakery Counter', true),
('aaaaaaaa-3333-4333-8333-333333333333', '33333333-3333-4333-8333-333333333333', 'Showroom', true)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    is_default = EXCLUDED.is_default;

INSERT INTO categories (id, business_id, name, slug) VALUES
('bbbbbbbb-1111-4111-8111-111111111111', '11111111-1111-4111-8111-111111111111', 'Produce', 'produce'),
('bbbbbbbb-1112-4112-8112-111111111112', '11111111-1111-4111-8111-111111111111', 'Pantry', 'pantry'),
('bbbbbbbb-2222-4222-8222-222222222222', '22222222-2222-4222-8222-222222222222', 'Bread', 'bread'),
('bbbbbbbb-2223-4223-8223-222222222223', '22222222-2222-4222-8222-222222222222', 'Pastries', 'pastries'),
('bbbbbbbb-3333-4333-8333-333333333333', '33333333-3333-4333-8333-333333333333', 'Plants', 'plants'),
('bbbbbbbb-3334-4334-8334-333333333334', '33333333-3333-4333-8333-333333333333', 'Care Kits', 'care-kits')
ON CONFLICT (business_id, slug) DO UPDATE SET
    name = EXCLUDED.name;

INSERT INTO products (
    id, business_id, category_id, name, slug, description, brand, unit,
    product_type, is_handmade, is_public, status
) VALUES
('cccccccc-1111-4111-8111-111111111111', '11111111-1111-4111-8111-111111111111', 'bbbbbbbb-1111-4111-8111-111111111111', 'Heirloom Tomato Box', 'heirloom-tomato-box', 'Seasonal tomato mix from nearby growers.', 'Mercado Verde', 'box', 'stocked_product', false, true, 'active'),
('cccccccc-1112-4112-8112-111111111112', '11111111-1111-4111-8111-111111111111', 'bbbbbbbb-1112-4112-8112-111111111112', 'House Salsa Verde', 'house-salsa-verde', 'Roasted tomatillo salsa prepared daily.', 'Mercado Verde', 'jar', 'stocked_product', true, true, 'active'),
('cccccccc-2222-4222-8222-222222222222', '22222222-2222-4222-8222-222222222222', 'bbbbbbbb-2222-4222-8222-222222222222', 'Country Sourdough', 'country-sourdough', 'Naturally leavened loaf with crisp crust and open crumb.', 'Casa Pan', 'loaf', 'stocked_product', true, true, 'active'),
('cccccccc-2223-4223-8223-222222222223', '22222222-2222-4222-8222-222222222222', 'bbbbbbbb-2223-4223-8223-222222222223', 'Guava Roll', 'guava-roll', 'Laminated pastry filled with guava jam.', 'Casa Pan', 'piece', 'stocked_product', true, true, 'active'),
('cccccccc-3333-4333-8333-333333333333', '33333333-3333-4333-8333-333333333333', 'bbbbbbbb-3333-4333-8333-333333333333', 'Monstera Deliciosa', 'monstera-deliciosa', 'Medium indoor monstera in nursery pot.', 'Taller Botanico', 'plant', 'unique_item', false, true, 'active'),
('cccccccc-3334-4334-8334-333333333334', '33333333-3333-4333-8333-333333333333', 'bbbbbbbb-3334-4334-8334-333333333334', 'Starter Plant Care Kit', 'starter-plant-care-kit', 'Soil mix, fertilizer, pruning snips, and care card.', 'Taller Botanico', 'kit', 'stocked_product', false, true, 'active')
ON CONFLICT (business_id, slug) DO UPDATE SET
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
    id, product_id, business_id, sku, barcode, internal_code, name, attributes,
    price, cost, currency, track_inventory, public_stock_status, reorder_point,
    lead_time_days, status
) VALUES
('dddddddd-1111-4111-8111-111111111111', 'cccccccc-1111-4111-8111-111111111111', '11111111-1111-4111-8111-111111111111', 'MV-TOMATO-BOX', '7501000000011', 'MV-TOMATO-BOX', '2 kg box', '{"size":"2 kg"}', 185.00, 98.00, 'MXN', true, 'available', 6, 2, 'active'),
('dddddddd-1112-4112-8112-111111111112', 'cccccccc-1112-4112-8112-111111111112', '11111111-1111-4111-8111-111111111111', 'MV-SALSA-VERDE', '7501000000012', 'MV-SALSA-VERDE', '450 g jar', '{"size":"450 g"}', 86.00, 34.00, 'MXN', true, 'low_stock', 12, 1, 'active'),
('dddddddd-2222-4222-8222-222222222222', 'cccccccc-2222-4222-8222-222222222222', '22222222-2222-4222-8222-222222222222', 'CP-SOURDOUGH', '7502000000021', 'CP-SOURDOUGH', '900 g loaf', '{"size":"900 g"}', 120.00, 46.00, 'MXN', true, 'available', 10, 1, 'active'),
('dddddddd-2223-4223-8223-222222222223', 'cccccccc-2223-4223-8223-222222222223', '22222222-2222-4222-8222-222222222222', 'CP-GUAVA-ROLL', '7502000000022', 'CP-GUAVA-ROLL', 'Single pastry', '{"size":"single"}', 52.00, 18.00, 'MXN', true, 'available', 20, 1, 'active'),
('dddddddd-3333-4333-8333-333333333333', 'cccccccc-3333-4333-8333-333333333333', '33333333-3333-4333-8333-333333333333', 'TB-MONSTERA-M', '7503000000031', 'TB-MONSTERA-M', 'Medium plant', '{"size":"medium"}', 420.00, 210.00, 'MXN', true, 'low_stock', 3, 5, 'active'),
('dddddddd-3334-4334-8334-333333333334', 'cccccccc-3334-4334-8334-333333333334', '33333333-3333-4333-8333-333333333333', 'TB-CARE-KIT', '7503000000032', 'TB-CARE-KIT', 'Starter kit', '{"size":"starter"}', 260.00, 122.00, 'MXN', true, 'available', 5, 3, 'active')
ON CONFLICT (business_id, sku) DO UPDATE SET
    barcode = EXCLUDED.barcode,
    internal_code = EXCLUDED.internal_code,
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
    updated_at = now();

INSERT INTO product_images (id, product_id, url, alt_text, position) VALUES
('eeeeeeee-1111-4111-8111-111111111111', 'cccccccc-1111-4111-8111-111111111111', 'https://images.unsplash.com/photo-1592924357228-91a4daadcfea?auto=format&fit=crop&w=900&q=80', 'Heirloom tomatoes in a produce box', 0),
('eeeeeeee-1112-4112-8112-111111111112', 'cccccccc-1112-4112-8112-111111111112', 'https://images.unsplash.com/photo-1626200419199-391ae4be7a41?auto=format&fit=crop&w=900&q=80', 'Jar of green salsa with tomatillos', 0),
('eeeeeeee-2222-4222-8222-222222222222', 'cccccccc-2222-4222-8222-222222222222', 'https://images.unsplash.com/photo-1585478259715-4d3f01d2954f?auto=format&fit=crop&w=900&q=80', 'Country sourdough loaf on a bakery table', 0),
('eeeeeeee-2223-4223-8223-222222222223', 'cccccccc-2223-4223-8223-222222222223', 'https://images.unsplash.com/photo-1509365465985-25d11c17e812?auto=format&fit=crop&w=900&q=80', 'Sweet pastry on a tray', 0),
('eeeeeeee-3333-4333-8333-333333333333', 'cccccccc-3333-4333-8333-333333333333', 'https://images.unsplash.com/photo-1614594975525-e45190c55d0b?auto=format&fit=crop&w=900&q=80', 'Monstera plant in a pot', 0),
('eeeeeeee-3334-4334-8334-333333333334', 'cccccccc-3334-4334-8334-333333333334', 'https://images.unsplash.com/photo-1416879595882-3373a0480b5b?auto=format&fit=crop&w=900&q=80', 'Plant care tools and soil on a table', 0)
ON CONFLICT (id) DO UPDATE SET
    url = EXCLUDED.url,
    alt_text = EXCLUDED.alt_text,
    position = EXCLUDED.position;

INSERT INTO stock_levels (business_id, variant_id, location_id, quantity_on_hand, quantity_reserved) VALUES
('11111111-1111-4111-8111-111111111111', 'dddddddd-1111-4111-8111-111111111111', 'aaaaaaaa-1111-4111-8111-111111111111', 24, 0),
('11111111-1111-4111-8111-111111111111', 'dddddddd-1112-4112-8112-111111111112', 'aaaaaaaa-1111-4111-8111-111111111111', 5, 0),
('22222222-2222-4222-8222-222222222222', 'dddddddd-2222-4222-8222-222222222222', 'aaaaaaaa-2222-4222-8222-222222222222', 18, 0),
('22222222-2222-4222-8222-222222222222', 'dddddddd-2223-4223-8223-222222222223', 'aaaaaaaa-2222-4222-8222-222222222222', 36, 0),
('33333333-3333-4333-8333-333333333333', 'dddddddd-3333-4333-8333-333333333333', 'aaaaaaaa-3333-4333-8333-333333333333', 2, 0),
('33333333-3333-4333-8333-333333333333', 'dddddddd-3334-4334-8334-333333333334', 'aaaaaaaa-3333-4333-8333-333333333333', 14, 0)
ON CONFLICT (business_id, variant_id, location_id) DO UPDATE SET
    quantity_on_hand = EXCLUDED.quantity_on_hand,
    quantity_reserved = EXCLUDED.quantity_reserved,
    updated_at = now();

COMMIT;
