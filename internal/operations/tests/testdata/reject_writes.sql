-- Faults apply only to the fixture marked for this specific table.
CREATE FUNCTION reject_operations_fixture_write() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE fixture_business text;
BEGIN
 fixture_business := COALESCE(to_jsonb(NEW)->>'business_id',to_jsonb(NEW)->'message'->>'business_id');
 IF EXISTS (SELECT 1 FROM businesses WHERE id::text=fixture_business AND description='operations-fail:'||TG_TABLE_NAME) THEN
  RAISE EXCEPTION 'operations fixture write failure';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER reject_operations_fixture_write BEFORE INSERT ON request_history FOR EACH ROW EXECUTE FUNCTION reject_operations_fixture_write();
CREATE TRIGGER reject_operations_fixture_write BEFORE INSERT ON request_access FOR EACH ROW EXECUTE FUNCTION reject_operations_fixture_write();
CREATE TRIGGER reject_operations_fixture_write BEFORE INSERT ON request_idempotency FOR EACH ROW EXECUTE FUNCTION reject_operations_fixture_write();
CREATE TRIGGER reject_operations_fixture_write BEFORE INSERT ON preorder_items FOR EACH ROW EXECUTE FUNCTION reject_operations_fixture_write();
CREATE TRIGGER reject_operations_fixture_write BEFORE INSERT ON pgmq.q_openlocal_jobs FOR EACH ROW EXECUTE FUNCTION reject_operations_fixture_write();
