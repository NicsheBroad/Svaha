CREATE TABLE profile_blocks (
    blocker text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    blocked text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    PRIMARY KEY (blocker, blocked),
    CHECK (blocker <> blocked)
);

CREATE INDEX profile_blocks_target_idx ON profile_blocks (blocked, blocker);

CREATE VIEW blocked_pairs AS
SELECT blocker AS viewer, blocked AS partner FROM profile_blocks
UNION ALL
SELECT blocked, blocker FROM profile_blocks;

CREATE TABLE profile_reports (
    reporter text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    subject text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    reason text NOT NULL CHECK (reason IN ('spam', 'harassment', 'underage', 'other')),
    details text NOT NULL CHECK (char_length(details) <= 1000),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (reporter, subject),
    CHECK (reporter <> subject)
);

CREATE OR REPLACE VIEW mutual_matches AS
SELECT r.viewer, r.candidate AS partner
FROM search_reactions AS r
JOIN search_reactions AS reverse
  ON reverse.viewer = r.candidate AND reverse.candidate = r.viewer
WHERE r.liked AND reverse.liked
  AND NOT EXISTS (
      SELECT 1 FROM blocked_pairs AS b WHERE b.viewer = r.viewer AND b.partner = r.candidate
  );

CREATE TABLE chat_reads (
    viewer text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    partner text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    last_read_id bigint NOT NULL CHECK (last_read_id > 0),
    PRIMARY KEY (viewer, partner),
    CHECK (viewer <> partner)
);

CREATE TABLE chat_send_requests (
    sender text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    nonce text NOT NULL,
    recipient text NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    PRIMARY KEY (sender, nonce),
    CHECK (sender <> recipient)
);

ALTER TABLE chat_messages ALTER COLUMN sent_at SET DEFAULT clock_timestamp();
