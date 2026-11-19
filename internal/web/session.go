package web

import (
	"errors"
	"log"
	"net/http"
	"time"

	"svaha/internal/postgres"
)

const sessionCookie = "svaha_session"
const sessionLifetime = 7 * 24 * time.Hour

// registerLogoutRoute подключает завершение сессии.
func registerLogoutRoute(mux *http.ServeMux, store *postgres.Store) {
	// Завершает сессию и возвращает пользователя на страницу входа.
	mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			if err := store.DeleteSession(r.Context(), cookie.Value); err != nil {
				log.Printf("Завершение сессии: %v", err)
				http.Error(w, "Выход временно недоступен.", http.StatusInternalServerError)
				return
			}
		}
		setSessionCookie(w, r, "", time.Unix(1, 0), -1)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
}

// withSession проверяет сессию перед выполнением защищённого обработчика.
func withSession(store *postgres.Store, handler func(http.ResponseWriter, *http.Request, postgres.Viewer)) http.HandlerFunc {
	// Проверяет cookie и передаёт данные пользователя обработчику.
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		viewer, err := store.SessionViewer(r.Context(), cookie.Value)
		if errors.Is(err, postgres.ErrSessionMissing) {
			setSessionCookie(w, r, "", time.Unix(1, 0), -1)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if err != nil {
			log.Printf("Проверка сессии: %v", err)
			http.Error(w, "Страница временно недоступна.", http.StatusInternalServerError)
			return
		}
		handler(w, r, viewer)
	}
}

// setSessionCookie записывает или удаляет cookie с токеном сессии.
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}
