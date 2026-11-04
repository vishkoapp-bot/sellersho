CREATE TABLE IF NOT EXISTS sms_logs (
 id UUID PRIMARY KEY,
 mobile_masked TEXT NOT NULL,
 message_type TEXT NOT NULL,
 provider TEXT NOT NULL,
 provider_message_id TEXT,
 status TEXT NOT NULL,
 error TEXT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS sms_logs_created_at_idx ON sms_logs(created_at DESC);