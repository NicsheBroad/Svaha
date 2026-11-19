package web

import (
	"bytes"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"time"
)

const maxPhotoBytes = 4 << 20
const maxPhotoSide = 2048

// serveProfilePhoto отдаёт фото или сообщает, что оно недоступно.
func serveProfilePhoto(w http.ResponseWriter, r *http.Request, photo []byte, err error) {
	if errors.Is(err, sql.ErrNoRows) || (err == nil && len(photo) == 0) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("Загрузка фото профиля: %v", err)
		http.Error(w, "Фото временно недоступно.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "profile.jpg", time.Time{}, bytes.NewReader(photo))
}

// uploadedPhoto проверяет загруженное изображение и преобразует его в JPEG без метаданных.
func uploadedPhoto(header *multipart.FileHeader) ([]byte, error) {
	file, err := header.Open()
	if err != nil {
		return nil, errors.New("Не удалось открыть фото.")
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxPhotoBytes+1))
	if err != nil || len(data) > maxPhotoBytes {
		return nil, errors.New("Максимальный размер фото — 4 МБ.")
	}
	return preparePhoto(data)
}

// preparePhoto проверяет размер исходника и преобразует JPEG или PNG в безопасный для показа JPEG.
func preparePhoto(data []byte) ([]byte, error) {
	if len(data) > maxPhotoBytes {
		return nil, errors.New("Максимальный размер фото — 4 МБ.")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, errors.New("Выберите фото в формате JPEG или PNG.")
	}
	if config.Width > maxPhotoSide || config.Height > maxPhotoSide {
		return nil, errors.New("Максимальное разрешение фото — 2048 × 2048 пикселей.")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("Фото повреждено или не открывается.")
	}
	// Убираем EXIF и сохраняем прозрачные участки PNG на белом фоне.
	canvas := image.NewRGBA(decoded.Bounds())
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(canvas, canvas.Bounds(), decoded, decoded.Bounds().Min, draw.Over)
	var photo bytes.Buffer
	if err := jpeg.Encode(&photo, canvas, nil); err != nil {
		return nil, errors.New("Не удалось обработать фото.")
	}
	return photo.Bytes(), nil
}

// photoNumber возвращает порядковый номер фотографии для подписи в шаблоне.
func photoNumber(index int) int {
	return index + 1
}
