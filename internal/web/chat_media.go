package web

import (
	"bytes"
	"errors"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"svaha/internal/postgres"
)

const maxChatAttachmentBytes = 20 << 20

// readChatAttachment проверяет размер и формат одного файла и готовит его для хранения.
func readChatAttachment(header *multipart.FileHeader, voice bool) (postgres.ChatAttachment, error) {
	if header.Size > maxChatAttachmentBytes {
		return postgres.ChatAttachment{}, errors.New("Размер файла должен быть не больше 20 МБ.")
	}
	file, err := header.Open()
	if err != nil {
		return postgres.ChatAttachment{}, errors.New("Не удалось открыть файл.")
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxChatAttachmentBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxChatAttachmentBytes {
		return postgres.ChatAttachment{}, errors.New("Файл пуст, повреждён или превышает 20 МБ.")
	}
	declared, _, _ := mime.ParseMediaType(header.Header.Get("Content-Type"))
	mediaType, extension := detectChatMedia(data, declared, path.Ext(strings.ToLower(header.Filename)), voice)
	if mediaType == "" {
		return postgres.ChatAttachment{}, errors.New("Выберите фото JPEG/PNG, аудио MP3/WAV/OGG/M4A/WebM или видео MP4/WebM.")
	}
	if voice && !strings.HasPrefix(mediaType, "audio/") {
		return postgres.ChatAttachment{}, errors.New("Голосовое сообщение должно быть аудиозаписью.")
	}
	if mediaType == "image/jpeg" {
		data, err = preparePhoto(data)
		if err != nil {
			return postgres.ChatAttachment{}, err
		}
	}
	return postgres.ChatAttachment{
		Name: chatAttachmentName(header.Filename, extension), MIME: mediaType, Data: data, Voice: voice,
	}, nil
}

// detectChatMedia определяет формат по заголовку файла и уточняет тип общих аудио- и видеоконтейнеров.
func detectChatMedia(data []byte, declared, extension string, voice bool) (string, string) {
	prefix := data[:min(len(data), 512)]
	detected := http.DetectContentType(prefix)
	if len(data) >= 12 && string(data[4:12]) == "ftypM4A " {
		return "audio/mp4", ".m4a"
	}
	switch detected {
	case "image/png", "image/jpeg":
		return "image/jpeg", ".jpg"
	case "audio/mpeg":
		return "audio/mpeg", ".mp3"
	case "audio/wave":
		return "audio/wav", ".wav"
	case "application/ogg":
		if bytes.Contains(prefix, []byte("OpusHead")) || bytes.Contains(prefix, []byte("\x01vorbis")) {
			return "audio/ogg", ".ogg"
		}
	case "video/mp4":
		if declared == "audio/mp4" || declared == "audio/x-m4a" || extension == ".m4a" || voice {
			return "audio/mp4", ".m4a"
		}
		return "video/mp4", ".mp4"
	case "video/webm":
		if !bytes.Contains(prefix, []byte("webm")) {
			break
		}
		if declared == "audio/webm" || voice {
			return "audio/webm", ".webm"
		}
		return "video/webm", ".webm"
	case "application/octet-stream":
		if len(data) >= 4 && data[0] == 0xff && data[1]&0xe0 == 0xe0 &&
			data[1]&0x18 != 0x08 && data[1]&0x06 != 0 && data[2]&0xf0 != 0 &&
			data[2]&0xf0 != 0xf0 && data[2]&0x0c != 0x0c {
			return "audio/mpeg", ".mp3"
		}
	}
	return "", ""
}

// chatAttachmentName сохраняет читаемое имя и подставляет расширение обнаруженного формата.
func chatAttachmentName(filename, extension string) string {
	name := path.Base(strings.ReplaceAll(filename, "\\", "/"))
	name = strings.TrimSpace(strings.TrimSuffix(name, path.Ext(name)))
	// Удаляет управляющие символы и повреждённые фрагменты имени файла.
	name = strings.Map(func(char rune) rune {
		if unicode.IsControl(char) || char == utf8.RuneError {
			return -1
		}
		return char
	}, name)
	if name == "" || name == "." || name == ".." {
		name = "вложение"
	}
	runes := []rune(name)
	return string(runes[:min(len(runes), 100)]) + extension
}

// serveChatAttachment отдаёт вложение участнику чата с поддержкой скачивания и перемотки.
func serveChatAttachment(w http.ResponseWriter, r *http.Request, attachment postgres.AttachmentMetadata, content io.ReadSeeker, err error) {
	if errors.Is(err, postgres.ErrMessageUnavailable) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("Загрузка вложения чата: %v", err)
		http.Error(w, "Файл временно недоступен.", http.StatusInternalServerError)
		return
	}

	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", attachment.MIME)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": attachment.Name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	http.ServeContent(w, r, attachment.Name, time.Time{}, content)
}
