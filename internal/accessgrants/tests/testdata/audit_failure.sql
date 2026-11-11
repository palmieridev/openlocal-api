CREATE FUNCTION {{function}}() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.business_id = '{{business}}'::uuid AND NEW.entity_type = 'access_grant' THEN
        RAISE EXCEPTION 'fixture rejects grant audit';
    END IF;
    RETURN NEW;
END
$$;
CREATE {{constraint}} TRIGGER {{trigger}} {{timing}} INSERT ON audit_logs
{{deferrable}}
FOR EACH ROW EXECUTE FUNCTION {{function}}();
