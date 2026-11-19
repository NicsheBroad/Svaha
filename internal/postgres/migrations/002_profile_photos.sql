ALTER TABLE users
    ADD COLUMN IF NOT EXISTS bio text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS hobbies text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS photo bytea;

CREATE TABLE IF NOT EXISTS profile_photos (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    content bytea NOT NULL
);

CREATE INDEX IF NOT EXISTS profile_photos_owner_idx ON profile_photos (username, id);

WITH migrated AS (
    INSERT INTO profile_photos (username, content)
    SELECT username, photo FROM users WHERE photo IS NOT NULL
    RETURNING username
)
UPDATE users SET photo = NULL FROM migrated WHERE users.username = migrated.username;
