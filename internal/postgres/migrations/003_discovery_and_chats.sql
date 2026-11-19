ALTER TABLE users ADD COLUMN IF NOT EXISTS search_id uuid NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX IF NOT EXISTS users_search_id_idx ON users (search_id);

CREATE TABLE IF NOT EXISTS search_reactions (
    viewer text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    candidate text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    liked boolean NOT NULL,
    PRIMARY KEY (viewer, candidate),
    CHECK (viewer <> candidate)
);

ALTER TABLE search_reactions
    ADD COLUMN IF NOT EXISTS reviewed_at timestamptz NOT NULL DEFAULT clock_timestamp();

CREATE OR REPLACE VIEW mutual_matches AS
SELECT r.viewer, r.candidate AS partner
FROM search_reactions AS r
JOIN search_reactions AS reverse
  ON reverse.viewer = r.candidate AND reverse.candidate = r.viewer
WHERE r.liked AND reverse.liked;

CREATE TABLE IF NOT EXISTS chat_messages (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    sender text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    recipient text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
    sent_at timestamptz NOT NULL DEFAULT now(),
    CHECK (sender <> recipient)
);

CREATE INDEX IF NOT EXISTS chat_messages_pair_idx ON chat_messages (sender, recipient, id DESC);

CREATE INDEX IF NOT EXISTS search_reactions_incoming_idx
    ON search_reactions (candidate, reviewed_at DESC) WHERE liked;

-- Для проверки наличия чата достаточно совпадения или сообщения в любом направлении.
CREATE OR REPLACE VIEW conversation_pairs AS
SELECT viewer, partner FROM mutual_matches
UNION ALL
SELECT sender, recipient FROM chat_messages
UNION ALL
SELECT recipient, sender FROM chat_messages;
