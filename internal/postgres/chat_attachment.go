package postgres

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"math"
)

const attachmentChunkSize = 256 << 10

type AttachmentMetadata struct {
	Name string
	MIME string
	Size int64
}

type attachmentReader struct {
	size        int64
	offset      int64
	chunk       []byte
	chunkOffset int64
	load        func(offset, length int) ([]byte, error)
}

const accessibleAttachment = `
  FROM mutual_matches AS mm
  JOIN users AS u ON u.username = mm.partner
  JOIN chat_messages AS m ON (
      (m.sender = mm.viewer AND m.recipient = mm.partner)
      OR (m.sender = mm.partner AND m.recipient = mm.viewer)
  )
  WHERE mm.viewer = $1 AND u.search_id = $2 AND m.id = $3 AND m.attachment_data IS NOT NULL`

// OpenMessageAttachment проверяет доступ к файлу и открывает чтение с перемоткой по частям.
func (s *Store) OpenMessageAttachment(ctx context.Context, username, partnerID string, id int64) (AttachmentMetadata, io.ReadSeeker, error) {
	var metadata AttachmentMetadata
	err := s.db.QueryRowContext(ctx,
		"SELECT m.attachment_name, m.attachment_type, octet_length(m.attachment_data)"+accessibleAttachment,
		username, partnerID, id).Scan(&metadata.Name, &metadata.MIME, &metadata.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return AttachmentMetadata{}, nil, ErrMessageUnavailable
	}
	if err != nil {
		return AttachmentMetadata{}, nil, err
	}

	reader := &attachmentReader{
		size: metadata.Size,
		// Проверяет доступ к каждому следующему фрагменту, в том числе после блокировки.
		load: func(offset, length int) ([]byte, error) {
			var chunk []byte
			err := s.db.QueryRowContext(ctx,
				"SELECT substring(m.attachment_data FROM $4::int FOR $5::int)"+accessibleAttachment,
				username, partnerID, id, offset+1, length).Scan(&chunk)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrMessageUnavailable
			}
			return chunk, err
		},
	}
	return metadata, reader, nil
}

// Read копирует байты из текущего фрагмента и подгружает следующий при необходимости.
func (r *attachmentReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.offset >= r.size {
		return 0, io.EOF
	}
	start := r.offset - r.chunkOffset
	if start < 0 || start >= int64(len(r.chunk)) {
		length := int(min(int64(attachmentChunkSize), r.size-r.offset))
		chunk, err := r.load(int(r.offset), length)
		if err != nil {
			return 0, err
		}
		if len(chunk) != length {
			return 0, io.ErrUnexpectedEOF
		}
		r.chunk, r.chunkOffset = chunk, r.offset
		start = 0
	}
	n := copy(p, r.chunk[start:])
	r.offset += int64(n)
	return n, nil
}

// Seek сдвигает позицию чтения для HTTP-запросов диапазона.
func (r *attachmentReader) Seek(offset int64, whence int) (int64, error) {
	var base int64
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = r.offset
	case io.SeekEnd:
		base = r.size
	default:
		return 0, errors.New("неизвестная позиция для перемотки")
	}
	if offset < -base || offset > math.MaxInt64-base {
		return 0, errors.New("недопустимая позиция в файле")
	}
	r.offset = base + offset
	return r.offset, nil
}
