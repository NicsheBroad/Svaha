package web

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"svaha/internal/postgres"
)

// registerSafetyRoutes добавляет жалобы и управление блокировками.
func registerSafetyRoutes(mux *http.ServeMux, store *postgres.Store) {
	// Показывает список заблокированных пользователем анкет.
	mux.HandleFunc("GET /profile/blocks", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		profiles, err := store.BlockedProfiles(r.Context(), viewer.Username)
		if err != nil {
			log.Printf("Загрузка блокировок: %v", err)
			http.Error(w, "Список блокировок временно недоступен.", http.StatusInternalServerError)
			return
		}
		renderPage(w, "blocks.html", http.StatusOK, profiles)
	}))

	// Устанавливает блокировку от имени текущего пользователя.
	mux.HandleFunc("POST /people/{id}/block", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		updateProfileBlock(w, r, store, viewer.Username, true)
	}))
	// Снимает блокировку от имени текущего пользователя.
	mux.HandleFunc("POST /profile/blocks/{id}/unblock", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		updateProfileBlock(w, r, store, viewer.Username, false)
	}))

	// Проверяет причину жалобы и сохраняет её для проверки модератором.
	mux.HandleFunc("POST /people/{id}/report", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		id := r.PathValue("id")
		if !publicProfileID.MatchString(id) {
			http.NotFound(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Не удалось прочитать жалобу.", http.StatusBadRequest)
			return
		}
		reason, details := r.PostForm.Get("reason"), strings.TrimSpace(r.PostForm.Get("details"))
		switch reason {
		case "spam", "harassment", "underage", "other":
		default:
			http.Error(w, "Укажите причину жалобы.", http.StatusBadRequest)
			return
		}
		if !validProfileText(details, 1000) {
			http.Error(w, "Комментарий должен содержать до 1000 символов.", http.StatusBadRequest)
			return
		}
		err := store.ReportProfile(r.Context(), viewer.Username, id, reason, details)
		switch {
		case errors.Is(err, postgres.ErrProfileUnavailable):
			http.NotFound(w, r)
		case err != nil:
			log.Printf("Сохранение жалобы: %v", err)
			http.Error(w, "Не удалось отправить жалобу.", http.StatusInternalServerError)
		default:
			http.Redirect(w, r, "/people/"+id+"?reported=1", http.StatusSeeOther)
		}
	}))
}

// updateProfileBlock сохраняет действие выбранного маршрута и возвращает к списку блокировок.
func updateProfileBlock(w http.ResponseWriter, r *http.Request, store *postgres.Store, username string, blocked bool) {
	id := r.PathValue("id")
	if !publicProfileID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	err := store.SetProfileBlock(r.Context(), username, id, blocked)
	switch {
	case errors.Is(err, postgres.ErrProfileUnavailable):
		http.NotFound(w, r)
	case err != nil:
		log.Printf("Изменение блокировки: %v", err)
		http.Error(w, "Не удалось изменить блокировку.", http.StatusInternalServerError)
	default:
		http.Redirect(w, r, "/profile/blocks", http.StatusSeeOther)
	}
}
