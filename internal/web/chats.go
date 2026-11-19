package web

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"svaha/internal/postgres"
)

type chatPage struct {
	postgres.Conversation
	Draft  string
	Notice string
	Latest bool
}

// registerChatRoutes добавляет список совпадений, просмотр переписки, отправку сообщений и удаление чата.
func registerChatRoutes(mux *http.ServeMux, store *postgres.Store) {
	// Показывает пользователей, с которыми лайк оказался взаимным.
	mux.HandleFunc("GET /chats", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		partners, err := store.ChatPartners(r.Context(), viewer.Username)
		if err != nil {
			log.Printf("Загрузка чатов: %v", err)
			http.Error(w, "Чаты временно недоступны.", http.StatusInternalServerError)
			return
		}
		renderPage(w, "chats.html", http.StatusOK, partners)
	}))

	// Открывает последние или более ранние сообщения выбранного чата.
	mux.HandleFunc("GET /chats/{id}", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		before, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		if err != nil || before < 0 {
			before = 0
		}
		renderConversation(w, r, store, viewer.Username, before, http.StatusOK, "", "")
	}))

	registerMessageRoutes(mux, store)

	// Удаляет чат для обоих участников и возвращает пользователя к списку чатов.
	mux.HandleFunc("POST /chats/{id}/delete", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		id := r.PathValue("id")
		if !publicProfileID.MatchString(id) {
			http.NotFound(w, r)
			return
		}
		err := store.DeleteConversation(r.Context(), viewer.Username, id)
		switch {
		case errors.Is(err, postgres.ErrChatUnavailable):
			http.NotFound(w, r)
		case err != nil:
			log.Printf("Удаление чата: %v", err)
			renderConversation(w, r, store, viewer.Username, 0, http.StatusInternalServerError, "", "Не удалось удалить чат.")
		default:
			http.Redirect(w, r, "/chats", http.StatusSeeOther)
		}
	}))
}

// renderConversation показывает доступную переписку вместе с формой отправки.
func renderConversation(w http.ResponseWriter, r *http.Request, store *postgres.Store, username string, before int64, status int, draft, notice string) {
	id := r.PathValue("id")
	if !publicProfileID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	chat, err := store.OpenConversation(r.Context(), username, id, before)
	switch {
	case errors.Is(err, postgres.ErrChatUnavailable):
		http.NotFound(w, r)
	case err != nil:
		log.Printf("Открытие переписки: %v", err)
		http.Error(w, "Чат временно недоступен.", http.StatusInternalServerError)
	default:
		renderPage(w, "chat.html", status, chatPage{Conversation: chat, Draft: draft, Notice: notice, Latest: before == 0})
	}
}
