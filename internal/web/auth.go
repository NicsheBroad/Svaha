package web

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"svaha/internal/postgres"
)

// registerAuthRoutes подключает вход, регистрацию и выход из аккаунта.
func registerAuthRoutes(mux *http.ServeMux, store *postgres.Store) {
	limiter := &loginLimiter{}
	// Показывает форму входа.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		renderPage(w, "login.html", http.StatusOK, "")
	})
	// Проверяет пароль и создаёт сессию при успешном входе.
	mux.HandleFunc("POST /{$}", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if r.ParseForm() != nil {
			renderPage(w, "login.html", http.StatusBadRequest, "Не удалось прочитать форму.")
			return
		}
		username := normalize(r.PostForm.Get("username"))
		password := r.PostForm.Get("password")
		if !validUsername(username) || password == "" || !utf8.ValidString(password) {
			renderPage(w, "login.html", http.StatusUnauthorized, "Неверный логин или пароль.")
			return
		}
		if wait := limiter.allow(time.Now(), r.RemoteAddr, username); wait > 0 {
			w.Header().Set("Retry-After", strconv.FormatInt(int64((wait+time.Second-1)/time.Second), 10))
			renderPage(w, "login.html", http.StatusTooManyRequests, "Слишком много попыток входа. Попробуйте позже.")
			return
		}
		ok, err := store.Authenticate(r.Context(), username, password)
		switch {
		case err != nil:
			log.Printf("Проверка входа: %v", err)
			renderPage(w, "login.html", http.StatusInternalServerError, "Вход временно недоступен.")
		case !ok:
			renderPage(w, "login.html", http.StatusUnauthorized, "Неверный логин или пароль.")
		default:
			expiresAt := time.Now().Add(sessionLifetime)
			token, err := store.CreateSession(r.Context(), username, expiresAt)
			if err != nil {
				log.Printf("Создание сессии: %v", err)
				renderPage(w, "login.html", http.StatusInternalServerError, "Вход временно недоступен.")
				return
			}
			setSessionCookie(w, r, token, expiresAt, int(sessionLifetime.Seconds()))
			http.Redirect(w, r, "/profile", http.StatusSeeOther)
		}
	})
	// Показывает форму регистрации.
	mux.HandleFunc("GET /register", func(w http.ResponseWriter, _ *http.Request) {
		renderPage(w, "register.html", http.StatusOK, "")
	})
	// Проверяет форму регистрации и создаёт аккаунт.
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if r.ParseForm() != nil {
			renderPage(w, "register.html", http.StatusBadRequest, "Не удалось прочитать форму.")
			return
		}
		form := r.PostForm
		user := readProfileForm(form, postgres.User{Username: normalize(form.Get("username"))})
		if notice := validateProfile(user); notice != "" {
			renderPage(w, "register.html", http.StatusBadRequest, notice)
			return
		}
		password := form.Get("password")
		if !validUsername(user.Username) ||
			!utf8.ValidString(password) || utf8.RuneCountInString(password) < 8 {
			renderPage(w, "register.html", http.StatusBadRequest, "Укажите логин. Пароль должен содержать не менее 8 символов.")
			return
		}
		err := store.Register(r.Context(), user, password)
		switch {
		case errors.Is(err, postgres.ErrDuplicate):
			renderPage(w, "register.html", http.StatusConflict, "Такой логин или email уже занят.")
		case err != nil:
			log.Printf("Регистрация: %v", err)
			renderPage(w, "register.html", http.StatusInternalServerError, "Регистрация временно недоступна.")
		default:
			renderPage(w, "login.html", http.StatusCreated, "Аккаунт создан. Теперь можно войти.")
		}
	})

	registerLogoutRoute(mux, store)
}

// normalize удаляет пробелы по краям и приводит значение к нижнему регистру.
func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// validUsername проверяет, что логин заполнен и может храниться в PostgreSQL.
func validUsername(username string) bool {
	return username != "" && utf8.ValidString(username) && !strings.ContainsRune(username, 0)
}
