ALTER TABLE chat_messages
    ADD COLUMN edited_at timestamptz,
    ADD COLUMN attachment_name text NOT NULL DEFAULT '',
    ADD COLUMN attachment_type text NOT NULL DEFAULT '',
    ADD COLUMN attachment_data bytea,
    ADD COLUMN is_voice boolean NOT NULL DEFAULT false;

ALTER TABLE chat_messages
    DROP CONSTRAINT IF EXISTS chat_messages_body_check,
    ADD CONSTRAINT chat_messages_content_check CHECK (
        char_length(body) <= 2000 AND (char_length(body) > 0 OR attachment_data IS NOT NULL)
    ),
    ADD CONSTRAINT chat_messages_attachment_check CHECK (
        (attachment_data IS NULL AND attachment_name = '' AND attachment_type = '' AND NOT is_voice)
        OR (
            attachment_data IS NOT NULL AND octet_length(attachment_data) BETWEEN 1 AND 20971520
            AND char_length(attachment_name) BETWEEN 1 AND 120
            AND attachment_type IN ('image/jpeg', 'audio/mpeg', 'audio/wav', 'audio/ogg',
                                    'audio/webm', 'audio/mp4', 'video/mp4', 'video/webm')
            AND (NOT is_voice OR attachment_type LIKE 'audio/%')
        )
    );
