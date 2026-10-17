-- +goose Up

-- The people who may use the admin. There is no public signup: rows are
-- created by cmd/adduser. Emails are stored lower-cased so the unique
-- constraint cannot be dodged by changing case.
CREATE TABLE admin_users (
  id            UUID PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE CHECK (email = lower(email) AND length(email) > 0),
  -- argon2id in PHC string format; the parameters travel with the hash.
  password_hash TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER admin_users_set_updated_at
  BEFORE UPDATE ON admin_users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The shape alexedwards/scs expects of a session store. token holds a
-- SHA-256 of the cookie value (HashTokenInStore), so reading this table
-- does not hand out live sessions.
CREATE TABLE sessions (
  token  TEXT PRIMARY KEY,
  data   BYTEA NOT NULL,
  expiry TIMESTAMPTZ NOT NULL
);

CREATE INDEX sessions_expiry_idx ON sessions (expiry);

-- +goose Down
DROP TABLE sessions;
DROP TABLE admin_users;
