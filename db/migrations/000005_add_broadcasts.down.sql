BEGIN;

DROP INDEX IF EXISTS idx_broadcast_sender_id;
DROP TABLE IF EXISTS broadcast;

COMMIT;