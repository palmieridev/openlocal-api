-- Only scripts/demo.py supplies verified development identity/org parameters.
BEGIN;
DO $$ BEGIN
 IF current_database()<>'openlocal_demo' OR (SELECT shobj_description(oid,'pg_database') FROM pg_database WHERE datname=current_database()) IS DISTINCT FROM 'openlocal-demo-v1-local-only' THEN RAISE EXCEPTION 'Refusing non-demo database'; END IF;
END $$;
SELECT pg_advisory_xact_lock(71403811);
CREATE TABLE IF NOT EXISTS demo_seed_state (version text PRIMARY KEY, seeded_at timestamptz NOT NULL DEFAULT now());
CREATE FUNCTION pg_temp.did(text) RETURNS uuid LANGUAGE sql IMMUTABLE AS $$ SELECT md5('openlocal-demo-v1:' || $1)::uuid $$;
CREATE FUNCTION pg_temp.ean(text) RETURNS text LANGUAGE sql IMMUTABLE AS $$ SELECT $1 || ((10 - sum(substr($1,i,1)::int * CASE WHEN i%2=0 THEN 3 ELSE 1 END)%10)%10)::text FROM generate_series(1,12) i $$;
\if :reset
DELETE FROM stock_movements WHERE business_id IN (pg_temp.did('tienda'),pg_temp.did('pan'),pg_temp.did('salon'));
DELETE FROM preorders WHERE business_id=pg_temp.did('pan');
DELETE FROM appointments WHERE business_id=pg_temp.did('salon');
DELETE FROM businesses WHERE id IN (pg_temp.did('tienda'),pg_temp.did('pan'),pg_temp.did('salon'));
DELETE FROM demo_seed_state WHERE version='v1';
\endif
SELECT NOT EXISTS(SELECT 1 FROM demo_seed_state WHERE version='v1') AS apply \gset
\if :apply
CREATE TEMP TABLE demo_identity AS SELECT :'owner'::text AS owner, :'tienda_org'::text AS tienda_org, :'pan_org'::text AS pan_org, :'salon_org'::text AS salon_org;
INSERT INTO users(id,clerk_user_id) SELECT pg_temp.did('owner:'||owner),owner FROM demo_identity ON CONFLICT(clerk_user_id) DO NOTHING;
CREATE TEMP TABLE demo_owner AS SELECT id FROM users WHERE clerk_user_id=:'owner';
INSERT INTO businesses(id,name,slug,description,business_type,status,clerk_org_id,address,neighborhood,city,state,country,postal_code,latitude,longitude,whatsapp,timezone)
SELECT pg_temp.did(k),n,'demo-'||k,'Negocio ficticio para demostración. Precios en MXN; no se realizan cobros.',t,'active',org,'Calle Ejemplo 123','Roma Norte','Ciudad de México','Ciudad de México','MX','06700',19.4194+offset_lat,-99.1591,'+525555550100','America/Mexico_City'
FROM demo_identity CROSS JOIN LATERAL (VALUES ('tienda','Abarrotes La Esquina','retail',tienda_org,0.0),('pan','Panadería Amanecer','comida',pan_org,0.002),('salon','Salón Jacaranda','servicios',salon_org,-0.002)) v(k,n,t,org,offset_lat);
INSERT INTO business_members(business_id,user_id,clerk_org_id,role) SELECT b.id,o.id,b.clerk_org_id,'owner' FROM businesses b CROSS JOIN demo_owner o WHERE b.slug IN ('demo-tienda','demo-pan','demo-salon');
INSERT INTO business_hours(business_id,day_of_week,opens_at,closes_at,is_closed)
SELECT pg_temp.did(k),d,'08:00'::time,'20:00'::time,false FROM unnest(ARRAY['tienda','pan','salon']) k CROSS JOIN generate_series(0,6) d;
INSERT INTO inventory_locations(id,business_id,name,is_default) SELECT pg_temp.did(k||':loc'),pg_temp.did(k),'Mostrador',true FROM unnest(ARRAY['tienda','pan','salon']) k;
INSERT INTO categories(id,business_id,name,slug) SELECT pg_temp.did(k||':cat:'||c),pg_temp.did(k),c,slug FROM (VALUES ('tienda','Despensa','despensa'),('tienda','Bebidas','bebidas'),('pan','Pan artesanal','pan-artesanal')) v(k,c,slug);
CREATE TEMP TABLE demo_catalog(k text,code text,n text,category text,price numeric,cost numeric,demand int,stock int);
INSERT INTO demo_catalog VALUES
('tienda','arroz','Arroz extra 1 kg','Despensa',32,21,400,75),('tienda','cafe','Café de olla 250 g','Despensa',95,55,100,4),
('tienda','frijol','Frijol negro 1 kg','Despensa',42,27,120,40),('tienda','leche','Leche entera 1 L','Bebidas',29,22,150,0),
('tienda','agua','Agua mineral 600 ml','Bebidas',18,10,60,30),('tienda','sal','Sal de mar 500 g','Despensa',22,11,10,12),
('pan','hogaza','Hogaza de masa madre','Pan artesanal',110,42,40,18),('pan','concha','Concha de vainilla','Pan artesanal',18,7,120,4);
INSERT INTO products(id,business_id,category_id,name,slug,description,unit,is_public,status,is_handmade)
SELECT pg_temp.did(code||':product'),pg_temp.did(k),pg_temp.did(k||':cat:'||category),n,code,'Elaborado y seleccionado en el barrio. Datos ficticios de demostración.','pieza',true,'active',k='pan' FROM demo_catalog;
INSERT INTO product_variants(id,business_id,product_id,sku,internal_code,barcode,name,price,cost,currency,reorder_point,lead_time_days,public_stock_status)
SELECT pg_temp.did(code||':variant'),pg_temp.did(k),pg_temp.did(code||':product'),'DEMO-'||upper(code),'DEMO-'||upper(code),pg_temp.ean('200000000'||lpad(row_number() over(order by code)::text,3,'0')),'Presentación regular',price,cost,'MXN',10,3,CASE WHEN stock=0 THEN 'out_of_stock' WHEN stock<=10 THEN 'low_stock' ELSE 'available' END FROM demo_catalog;
-- Second presentation shares a product, with its own barcode and ledger.
INSERT INTO product_variants(id,business_id,product_id,sku,internal_code,barcode,name,price,cost,reorder_point,lead_time_days,public_stock_status)
VALUES(pg_temp.did('arroz2:variant'),pg_temp.did('tienda'),pg_temp.did('arroz:product'),'DEMO-ARROZ2','DEMO-ARROZ2',pg_temp.ean('200000000099'),'Bolsa 2 kg',60,40,8,3,'available');
INSERT INTO demo_catalog VALUES ('tienda','arroz2','Arroz 2 kg','Despensa',60,40,30,25);
-- Opening receipt, distributed sales across 90 days, and count corrections. All levels
-- are derived from the ledger inside this same transaction, never independent fixtures.
INSERT INTO stock_movements(id,business_id,variant_id,location_id,movement_type,quantity,unit_cost,created_by,created_at,reference_type)
SELECT pg_temp.did(code||':opening'),pg_temp.did(k),pg_temp.did(code||':variant'),pg_temp.did(k||':loc'),CASE WHEN k='pan' THEN 'IN_PRODUCTION' ELSE 'IN_PURCHASE' END,demand+stock+2,cost,o.id,now()-interval '91 days','demo' FROM demo_catalog CROSS JOIN demo_owner o;
INSERT INTO stock_movements(id,business_id,variant_id,location_id,movement_type,quantity,unit_cost,created_by,created_at,reference_type)
SELECT pg_temp.did(code||':sale:'||d),pg_temp.did(k),pg_temp.did(code||':variant'),pg_temp.did(k||':loc'),'OUT_SALE',demand/10.0,cost,o.id,now()-make_interval(days=>d*8),'demo' FROM demo_catalog CROSS JOIN generate_series(1,10) d CROSS JOIN demo_owner o;
INSERT INTO stock_movements(id,business_id,variant_id,location_id,movement_type,quantity,created_by,created_at,reference_type)
SELECT pg_temp.did(code||':adjust'),pg_temp.did(k),pg_temp.did(code||':variant'),pg_temp.did(k||':loc'),'OUT_ADJUSTMENT',2,o.id,now()-interval '1 hour','demo' FROM demo_catalog CROSS JOIN demo_owner o;
INSERT INTO stock_levels(business_id,variant_id,location_id,quantity_on_hand)
SELECT business_id,variant_id,location_id,sum(CASE WHEN movement_type LIKE 'IN_%' THEN quantity ELSE -quantity END) FROM stock_movements WHERE reference_type='demo' GROUP BY 1,2,3;
INSERT INTO business_subscriptions(business_id,tier,status,modules,period_start,period_end)
SELECT pg_temp.did(k),'business','pilot',ARRAY[m],date_trunc('day',now())-interval '30 days',date_trunc('day',now())+interval '30 days' FROM (VALUES ('pan','preorders'),('salon','appointments')) v(k,m);
INSERT INTO business_modules(business_id,module,enabled,settings)
SELECT pg_temp.did(k),m,true,jsonb_build_object('enabled',true,'hours',(SELECT jsonb_agg(jsonb_build_object('day_of_week',d,'opens_at','08:00','closes_at','20:00','is_closed',false)) FROM generate_series(0,6) d),'closed_dates','[]'::jsonb,'lead_minutes',60,'horizon_days',30,'slot_minutes',30,'cancel_before_minutes',60,'pending_ttl_minutes',1440,'daily_cutoff','20:00','max_orders_per_slot',12,'variant_rules',CASE WHEN k='pan' THEN (SELECT jsonb_agg(jsonb_build_object('variant_id',pg_temp.did(code||':variant'),'min_quantity','2','max_quantity','24','step','2')) FROM demo_catalog WHERE k='pan') ELSE '[]'::jsonb END)
FROM (VALUES ('pan','preorders'),('salon','appointments')) v(k,m);
INSERT INTO services(id,business_id,name,description,duration_minutes,price,buffer_minutes) VALUES
(pg_temp.did('corte'),pg_temp.did('salon'),'Corte y peinado','Asesoría y acabado incluidos',60,350,15),
(pg_temp.did('manicure'),pg_temp.did('salon'),'Manicure clásico','Cuidado de manos y esmaltado',30,220,15);
INSERT INTO appointment_resources(id,business_id,name,service_ids) VALUES
(pg_temp.did('ana'),pg_temp.did('salon'),'Ana · estilista',ARRAY[pg_temp.did('corte'),pg_temp.did('manicure')]),
(pg_temp.did('luz'),pg_temp.did('salon'),'Luz · estilista',ARRAY[pg_temp.did('corte')]);
INSERT INTO module_customers(id,business_id,phone,name) SELECT pg_temp.did(k||':customer:'||i),pg_temp.did(k),'+5255555501'||lpad(i::text,2,'0'),(ARRAY['María Demo','Luis Demo','Sofía Demo','Diego Demo','Elena Demo','Pablo Demo','Lucía Demo'])[i] FROM unnest(ARRAY['pan','salon']) k CROSS JOIN generate_series(1,7) i;
CREATE TEMP TABLE demo_preorder AS SELECT s,i,((now() AT TIME ZONE 'America/Mexico_City')::date+CASE WHEN i<=4 THEN i::int ELSE -i::int END+time '10:00') AT TIME ZONE 'America/Mexico_City' AS t FROM unnest(ARRAY['requested','confirmed','preparing','ready','collected','cancelled','uncollected']) WITH ORDINALITY v(s,i);
INSERT INTO preorder_pickup_windows(id,business_id,starts_at,ends_at) SELECT pg_temp.did('window:'||i),pg_temp.did('pan'),t,t+interval '30 minutes' FROM demo_preorder;
INSERT INTO preorders(id,business_id,customer_id,pickup_window_id,customer_name,customer_phone,starts_at,ends_at,timezone,estimated_total,final_total,status,expires_at,confirmed_at,created_at,customer_user_id)
SELECT pg_temp.did('preorder:'||i),pg_temp.did('pan'),c.id,pg_temp.did('window:'||i),c.name,c.phone,t,t+interval '30 minutes','America/Mexico_City',220,CASE WHEN s='collected' THEN 220 END,s,t-interval '1 hour',CASE WHEN s NOT IN ('requested','cancelled') THEN least(now()-interval '1 hour',t-interval '2 days')+interval '2 minutes' END,least(now()-interval '1 hour',t-interval '2 days'),o.id FROM demo_preorder v JOIN module_customers c ON c.id=pg_temp.did('pan:customer:'||i) CROSS JOIN demo_owner o;
INSERT INTO preorder_items(id,business_id,preorder_id,variant_id,name,unit,quantity,unit_price,total,final_quantity) SELECT pg_temp.did('item:'||i),pg_temp.did('pan'),pg_temp.did('preorder:'||i),pg_temp.did('hogaza:variant'),'Hogaza de masa madre','pieza',2,110,220,CASE WHEN s='collected' THEN 2 END FROM demo_preorder;
CREATE TEMP TABLE demo_appointment AS SELECT s,i,((now() AT TIME ZONE 'America/Mexico_City')::date+CASE WHEN i<=2 THEN i::int ELSE -i::int END+time '12:00') AT TIME ZONE 'America/Mexico_City' AS t FROM unnest(ARRAY['requested','confirmed','completed','cancelled','no_show']) WITH ORDINALITY v(s,i);
INSERT INTO appointments(id,business_id,customer_id,service_id,resource_id,customer_name,customer_phone,service_name,duration_minutes,buffer_minutes,price,starts_at,ends_at,timezone,status,expires_at,confirmed_at,created_at,customer_user_id)
SELECT pg_temp.did('appointment:'||i),pg_temp.did('salon'),c.id,pg_temp.did('corte'),pg_temp.did('ana'),c.name,c.phone,'Corte y peinado',60,15,350,t,t+interval '1 hour','America/Mexico_City',s,t-interval '1 hour',CASE WHEN s NOT IN ('requested','cancelled') THEN least(now()-interval '1 hour',t-interval '2 days')+interval '2 minutes' END,least(now()-interval '1 hour',t-interval '2 days'),o.id FROM demo_appointment v JOIN module_customers c ON c.id=pg_temp.did('salon:customer:'||i) CROSS JOIN demo_owner o;
-- Full transition chains, quota events only once confirmed (as in the application).
CREATE TEMP TABLE demo_records AS SELECT id,business_id,status,created_at,starts_at,'preorders'::text AS module FROM preorders WHERE business_id=pg_temp.did('pan') UNION ALL SELECT id,business_id,status,created_at,starts_at,'appointments' FROM appointments WHERE business_id=pg_temp.did('salon');
INSERT INTO request_history(business_id,appointment_id,preorder_id,from_status,to_status,actor,at)
SELECT business_id,CASE WHEN module='appointments' THEN id END,CASE WHEN module='preorders' THEN id END,CASE WHEN step=1 THEN '' ELSE chain[step-1] END,chain[step],CASE WHEN step=1 THEN 'customer' ELSE 'owner' END,created_at+make_interval(mins=>step)
FROM demo_records CROSS JOIN LATERAL (SELECT CASE WHEN status='requested' THEN ARRAY['requested'] WHEN status='cancelled' THEN ARRAY['requested','cancelled'] WHEN module='appointments' THEN CASE WHEN status='confirmed' THEN ARRAY['requested','confirmed'] ELSE ARRAY['requested','confirmed',status] END ELSE CASE status WHEN 'confirmed' THEN ARRAY['requested','confirmed'] WHEN 'preparing' THEN ARRAY['requested','confirmed','preparing'] WHEN 'ready' THEN ARRAY['requested','confirmed','preparing','ready'] WHEN 'collected' THEN ARRAY['requested','confirmed','preparing','ready','collected'] ELSE ARRAY['requested','confirmed','preparing','ready','uncollected'] END END AS chain) ch CROSS JOIN LATERAL generate_subscripts(chain,1) step;
UPDATE preorders p SET version=(SELECT count(*) FROM request_history h WHERE h.preorder_id=p.id) WHERE business_id=pg_temp.did('pan');
UPDATE appointments a SET version=(SELECT count(*) FROM request_history h WHERE h.appointment_id=a.id) WHERE business_id=pg_temp.did('salon');
INSERT INTO module_usage_events(business_id,module,record_id,period_start,created_at) SELECT business_id,module,id,date_trunc('day',now())-interval '30 days',created_at+interval '2 minutes' FROM demo_records WHERE status NOT IN ('requested','cancelled');
INSERT INTO saved_businesses(user_id,business_id) SELECT o.id,pg_temp.did(k) FROM demo_owner o CROSS JOIN unnest(ARRAY['tienda','pan','salon']) k ON CONFLICT DO NOTHING;
INSERT INTO demo_seed_state(version) VALUES('v1');
\endif
REFRESH MATERIALIZED VIEW product_sales_summary;
COMMIT;
SELECT 'Demo baseline ready (existing baseline preserved unless reset requested).';
