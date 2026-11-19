CREATE TABLE IF NOT EXISTS users (
    username text PRIMARY KEY,
    email text NOT NULL UNIQUE,
    first_name text NOT NULL,
    phone text NOT NULL,
    birth_date date NOT NULL,
    city text NOT NULL,
    gender text NOT NULL,
    looking_for text NOT NULL,
    password_salt bytea NOT NULL,
    password_hash bytea NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash bytea PRIMARY KEY,
    username text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS sessions_expires_at_idx ON sessions (expires_at);
