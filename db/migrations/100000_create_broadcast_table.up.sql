BEGIN;

CREATE TABLE broadcast (
    id BIGSERIAL PRIMARY KEY,
    sender_id BIGINT NOT NULL,
    message TEXT NOT NULL,
    sent_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    status VARCHAR(20) NOT NULL
);

CREATE INDEX idx_broadcast_sender_id ON broadcast USING hash (sender_id);

COMMIT;