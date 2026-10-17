-- +goose Up

-- Bumped on every password change. Each session records the version it
-- logged in with, and one that no longer matches is turned away: changing
-- the password logs out every other device. A counter rather than a time,
-- so the clocks of the app and the database never have to agree.
ALTER TABLE admin_users ADD COLUMN password_version INTEGER NOT NULL DEFAULT 0;

-- Links sent by "Esqueci minha senha". token_hash is a SHA-256 of the
-- token in the link, so reading this table does not hand out resets. A
-- row is deleted when used, which is what makes a link single-use.
CREATE TABLE password_resets (
  token_hash BYTEA PRIMARY KEY,
  user_id    UUID NOT NULL REFERENCES admin_users (id) ON DELETE CASCADE,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Serves both the per-account limit on new links and deleting a user's
-- links once one is used.
CREATE INDEX password_resets_user_id_created_at_idx ON password_resets (user_id, created_at);

-- +goose Down
DROP TABLE password_resets;
ALTER TABLE admin_users DROP COLUMN password_version;
