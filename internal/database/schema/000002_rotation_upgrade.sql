-- Upgrade pre-Phase-01 api_keys in place, preserving existing keys and usage.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS secondary_key_hash VARCHAR(64);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS rotation_deadline TIMESTAMP WITH TIME ZONE;
-- Generated API key prefixes are longer than the legacy VARCHAR(16).
ALTER TABLE api_keys ALTER COLUMN key_prefix TYPE VARCHAR(64);
CREATE INDEX IF NOT EXISTS idx_api_keys_secondary_hash ON api_keys(secondary_key_hash);
