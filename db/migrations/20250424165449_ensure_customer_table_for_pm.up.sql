-- Ensure the customer table exists with all necessary fields
CREATE TABLE IF NOT EXISTS customer (
    id SERIAL PRIMARY KEY,
    telegram_id BIGINT NOT NULL UNIQUE,
    expire_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    subscription_link TEXT NULL,
    language TEXT NOT NULL DEFAULT 'en'
);

-- Add any missing columns that might be needed for the broadcast functionality
DO $$
BEGIN
    -- Check if language column exists, if not add it
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name='customer' AND column_name='language') THEN
        ALTER TABLE customer ADD COLUMN language TEXT NOT NULL DEFAULT 'en';
    END IF;

    -- Ensure created_at has default value
    IF EXISTS (SELECT 1 FROM information_schema.columns 
               WHERE table_name='customer' AND column_name='created_at' 
               AND column_default IS NULL) THEN
        ALTER TABLE customer ALTER COLUMN created_at SET DEFAULT NOW();
    END IF;
END $$;

-- Create an index on telegram_id for faster lookups
CREATE INDEX IF NOT EXISTS idx_customer_telegram_id ON customer(telegram_id);

COMMENT ON TABLE customer IS 'Stores customer information for the Telegram bot, used for subscription management and broadcast messages';