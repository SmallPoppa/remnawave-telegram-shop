-- We don't want to drop the customer table in down migration
-- as it might contain important customer data
-- But we can remove indexes we added

DROP INDEX IF EXISTS idx_customer_telegram_id;

-- Remove comments added in up migration
COMMENT ON TABLE customer IS NULL;