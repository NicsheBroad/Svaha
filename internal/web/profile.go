package web

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"svaha/internal/postgres"
)

type profilePage struct {
	postgres.User
	Initial string
}

type profileEditPage struct {
	postgres.User
	Notice    string
	MaxPhotos int
}

// registerProfileRoutes подключает просмотр, редактирование и фотографии собственной карточки.
func registerProfileRoutes(mux *http.ServeMux, store *postgres.Store) {
	// Показывает личную карточку пользователя.
	mux.HandleFunc("GET /profile", withOwnProfile(store, func(w http.ResponseWriter, _ *http.Request, user postgres.User) {
		initial, _ := utf8.DecodeRuneInString(user.FirstName)
		renderPage(w, "profile.html", http.StatusOK, profilePage{User: user, Initial: strings.ToUpper(string(initial))})
	}))
	// Показывает форму с текущими данными анкеты.
	mux.HandleFunc("GET /profile/edit", withOwnProfile(store, func(w http.ResponseWriter, _ *http.Request, user postgres.User) {
		renderProfileEdit(w, http.StatusOK, user, "")
	}))
	// Обрабатывает отправку формы редактирования.
	mux.HandleFunc("POST /profile/edit", withOwnProfile(store, func(w http.ResponseWriter, r *http.Request, user postgres.User) {
		saveProfile(w, r, store, user)
	}))
	// Отдаёт фото владельцу активной сессии.
	mux.HandleFunc("GET /profile/photo/{id}", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			http.NotFound(w, r)
			return
		}
		photo, err := store.ProfilePhoto(r.Context(), viewer.Username, id)
		serveProfilePhoto(w, r, photo, err)
	}))
}

// withOwnProfile загружает полную анкету для личных страниц после проверки сессии.
func withOwnProfile(store *postgres.Store, handler func(http.ResponseWriter, *http.Request, postgres.User)) http.HandlerFunc {
	// Загружает карточку текущего пользователя и передаёт её обработчику.
	return withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		user, err := store.OwnProfile(r.Context(), viewer.Username)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			http.NotFound(w, r)
		case err != nil:
			log.Printf("Загрузка личной анкеты: %v", err)
			http.Error(w, "Анкета временно недоступна.", http.StatusInternalServerError)
		default:
			handler(w, r, user)
		}
	})
}

// saveProfile читает форму, проверяет введённые данные и сохраняет изменения.
func saveProfile(w http.ResponseWriter, r *http.Request, store *postgres.Store, user postgres.User) {
	r.Body = http.MaxBytesReader(w, r.Body, postgres.MaxProfilePhotos*maxPhotoBytes+(64<<10))
	if err := r.ParseMultipartForm(maxPhotoBytes); err != nil {
		renderProfileEdit(w, http.StatusBadRequest, user, "Не удалось прочитать форму. Можно загрузить до 10 фото, каждое до 4 МБ.")
		return
	}
	defer r.MultipartForm.RemoveAll()

	form := r.PostForm
	user = readProfileForm(form, user)
	user.Bio = strings.TrimSpace(form.Get("bio"))
	user.Hobbies = strings.TrimSpace(form.Get("hobbies"))
	if notice := validateProfile(user); notice != "" {
		renderProfileEdit(w, http.StatusBadRequest, user, notice)
		return
	}

	files := r.MultipartForm.File["photos"]
	if len(files) > postgres.MaxProfilePhotos {
		renderProfileEdit(w, http.StatusBadRequest, user, postgres.ErrPhotoLimit.Error())
		return
	}

	var removeIDs []int64
	for _, value := range form["remove_photo"] {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id < 1 || len(removeIDs) == postgres.MaxProfilePhotos {
			renderProfileEdit(w, http.StatusBadRequest, user, "Проверьте список фото для удаления.")
			return
		}
		removeIDs = append(removeIDs, id)
	}

	photos := make([][]byte, 0, len(files))
	for index, file := range files {
		photo, err := uploadedPhoto(file)
		if err != nil {
			notice := fmt.Sprintf("Фото %d. %s", index+1, err)
			renderProfileEdit(w, http.StatusBadRequest, user, notice)
			return
		}
		photos = append(photos, photo)
	}
	err := store.UpdateProfile(r.Context(), user, photos, removeIDs)
	switch {
	case errors.Is(err, postgres.ErrPhotoLimit):
		renderProfileEdit(w, http.StatusBadRequest, user, err.Error())
	case errors.Is(err, postgres.ErrDuplicate):
		renderProfileEdit(w, http.StatusConflict, user, "Этот email уже используется.")
	case err != nil:
		log.Printf("Сохранение профиля: %v", err)
		renderProfileEdit(w, http.StatusInternalServerError, user, "Не удалось сохранить профиль.")
	default:
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
	}
}

// renderProfileEdit показывает форму анкеты с текущей галереей и сообщением об ошибке.
func renderProfileEdit(w http.ResponseWriter, status int, user postgres.User, notice string) {
	renderPage(w, "profile_edit.html", status, profileEditPage{
		User: user, Notice: notice, MaxPhotos: postgres.MaxProfilePhotos,
	})
}

// readProfileForm заполняет поля анкеты, общие для регистрации и редактирования.
func readProfileForm(form url.Values, user postgres.User) postgres.User {
	user.FirstName = strings.TrimSpace(form.Get("first_name"))
	user.Email = normalize(form.Get("email"))
	user.Phone = strings.TrimSpace(form.Get("phone"))
	user.BirthDate = form.Get("birth_date")
	user.City = strings.TrimSpace(form.Get("city"))
	user.Gender = form.Get("gender")
	user.LookingFor = form.Get("looking_for")
	return user
}

// validateProfile проверяет данные анкеты и возвращает сообщение при ошибке.
func validateProfile(user postgres.User) string {
	email, emailErr := mail.ParseAddress(user.Email)
	birthDate, birthErr := time.Parse(time.DateOnly, user.BirthDate)
	if user.FirstName == "" || !validProfileText(user.FirstName, 100) ||
		emailErr != nil || email.Address != user.Email || len(user.Email) > 254 || !validProfileText(user.Email, 254) ||
		birthErr != nil || birthDate.Year() < 1 || birthDate.After(time.Now()) {
		return "Проверьте имя, email и дату рождения."
	}
	switch user.Gender {
	case "female", "male", "other", "undisclosed":
	default:
		return "Выберите пол или вариант «Предпочитаю не указывать»."
	}
	switch user.LookingFor {
	case "", "women", "men", "anyone":
	default:
		return "Проверьте поле «Кого вы ищете»."
	}
	if !validProfileText(user.Phone, 40) || !validProfileText(user.City, 100) ||
		!validProfileText(user.Bio, 1500) || !validProfileText(user.Hobbies, 500) {
		return "Проверьте длину и содержимое полей анкеты."
	}
	return ""
}

// validProfileText проверяет кодировку, допустимые символы и длину текстового поля.
func validProfileText(value string, maxLength int) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= maxLength
}
