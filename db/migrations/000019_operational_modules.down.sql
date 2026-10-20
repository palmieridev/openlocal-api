DROP TABLE billing_operations, billing_events, request_idempotency, module_usage_events, request_history, request_access, preorder_items, preorders, preorder_pickup_windows, appointments, appointment_resources, services, module_customers, business_modules, business_subscriptions;
ALTER TABLE product_variants DROP CONSTRAINT variants_business_id_unique;
