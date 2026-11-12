-- The hardened view has the same columns and indexes as migration 8. Older
-- application versions can use it unchanged; retain its tenant predicates on
-- downgrade rather than reintroduce cross-business aggregate disclosure.
SELECT 1;
