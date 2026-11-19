package web

import (
	"bytes"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"svaha/internal/postgres"
)

// TestChatMediaTypes проверяет определение медиафайлов и отклонение текста с поддельным типом.
func TestChatMediaTypes(t *testing.T) {
	for _, item := range []struct {
		data, declared, extension, expected string
		voice                               bool
	}{
		{"OggS\x00OpusHead", "audio/ogg", ".ogg", "audio/ogg", true},
		{"\x1a\x45\xdf\xa3webm", "audio/webm", ".webm", "audio/webm", true},
		{"\x1a\x45\xdf\xa3webm", "video/webm", ".webm", "video/webm", false},
		{"\x00\x00\x00\x0cftypM4A ", "audio/mp4", ".m4a", "audio/mp4", true},
		{"\xff\xfb\x90\x00", "", ".mp3", "audio/mpeg", false},
		{"<html>text</html>", "image/jpeg", ".jpg", "", false},
	} {
		actual, _ := detectChatMedia([]byte(item.data), item.declared, item.extension, item.voice)
		if actual != item.expected {
			t.Errorf("для %q получен тип %q, ожидался %q", item.declared, actual, item.expected)
		}
	}
}

// TestChatPhotoUpload проверяет преобразование загруженного PNG и безопасное имя файла.
func TestChatPhotoUpload(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="attachment"; filename="picture.png"`},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(part, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	form, err := multipart.NewReader(&body, writer.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer form.RemoveAll()
	photo, err := readChatAttachment(form.File["attachment"][0], false)
	if err != nil {
		t.Fatal(err)
	}
	if photo.MIME != "image/jpeg" || photo.Name != "picture.jpg" || len(photo.Data) == 0 {
		t.Fatal("фото не преобразовано в JPEG")
	}
	if _, err := readChatAttachment(form.File["attachment"][0], true); err == nil {
		t.Fatal("фото принято как голосовая запись")
	}
}

// TestChatAttachmentPartialResponse проверяет выдачу диапазона для перемотки видео.
func TestChatAttachmentPartialResponse(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/attachment", nil)
	request.Header.Set("Range", "bytes=2-5")
	response := httptest.NewRecorder()
	serveChatAttachment(response, request,
		postgres.AttachmentMetadata{Name: "test.mp4", MIME: "video/mp4", Size: 10},
		bytes.NewReader([]byte("0123456789")), nil)
	if response.Code != http.StatusPartialContent || response.Body.String() != "2345" {
		t.Fatalf("неверный ответ на Range: %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Errorf("неверный Content-Range: %q", response.Header().Get("Content-Range"))
	}
}
