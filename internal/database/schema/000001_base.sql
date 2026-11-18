-- Canonical schema authority. Safe to run against a database created by the
-- legacy root migrations/000001_init_schema.up.sql (do not drop existing data).
CREATE TABLE IF NOT EXISTS api_keys (
  id VARCHAR(64) PRIMARY KEY,
  name VARCHAR(255) NOT NULL,
  key_prefix VARCHAR(64) NOT NULL,
  key_hash VARCHAR(64) NOT NULL UNIQUE,
  scopes TEXT[] NOT NULL DEFAULT '{}',
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  last_used_at TIMESTAMP WITH TIME ZONE,
  expires_at TIMESTAMP WITH TIME ZONE
);
CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash);
CREATE TABLE IF NOT EXISTS usage_records (
  id BIGSERIAL PRIMARY KEY,
  key_id VARCHAR(64) NOT NULL,
  capability VARCHAR(64) NOT NULL,
  provider VARCHAR(64) NOT NULL,
  status_code INT NOT NULL,
  duration_ms INT NOT NULL,
  request_id VARCHAR(64) NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_usage_key_id ON usage_records(key_id);
CREATE INDEX IF NOT EXISTS idx_usage_created_at ON usage_records(created_at);
