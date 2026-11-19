package postgres

import (
	"bytes"
	"io"
	"testing"
)

// TestAttachmentReader проверяет чтение диапазонов без загрузки файла целиком.
func TestAttachmentReader(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), 100000)
	var requests int
	reader := &attachmentReader{
		size: int64(len(data)),
		// Проверяет, что каждый запрос ограничен размером фрагмента.
		load: func(offset, length int) ([]byte, error) {
			requests++
			if length > attachmentChunkSize || offset+length > len(data) {
				t.Fatal("недопустимый диапазон чтения")
			}
			return data[offset : offset+length], nil
		},
	}
	if _, err := reader.Seek(12345, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	part := make([]byte, 64)
	if _, err := io.ReadFull(reader, part); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(part, data[12345:12345+len(part)]) {
		t.Fatal("чтение вернуло другой диапазон")
	}
	if _, err := reader.Seek(12350, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(reader, part); err != nil || requests != 1 {
		t.Fatalf("данные из текущего фрагмента должны читаться без нового запроса: %v", err)
	}
	if _, err := reader.Seek(-4, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	tail, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(tail, data[len(data)-4:]) || requests != 2 {
		t.Fatalf("не удалось прочитать остаток файла: %v", err)
	}
}
