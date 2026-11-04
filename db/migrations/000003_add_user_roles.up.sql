-- Existing accounts become read-only viewers. Admins are designated through
-- the ADMIN_EMAILS setting, which the API applies at every startup.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS role VARCHAR(10) NOT NULL DEFAULT 'viewer'
    CHECK (role IN ('admin', 'viewer'));
