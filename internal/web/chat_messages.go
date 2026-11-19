package web

import (
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"svaha/internal/postgres"
)

const maxMessageLength = 2000

var messageNonce = regexp.MustCompile(`^[A-Z2-7]{26,64}$`)

type messageEditPage struct {
	PartnerID string
	Message   postgres.ChatMessage
	Notice    string
}

// registerMessageRoutes добавляет отправку, правку, удаление сообщений и доступ к вложениям.
func registerMessageRoutes(mux *http.ServeMux, store *postgres.Store) {
	// Отправляет введённый текст, выбранный файл или запись с микрофона.
	mux.HandleFunc("POST /chats/{id}", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		sendMessage(w, r, store, viewer.Username)
	}))

	// Показывает форму редактирования собственного сообщения.
	mux.HandleFunc("GET /chats/{id}/messages/{messageID}/edit", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		editMessage(w, r, store, viewer.Username)
	}))

	// Сохраняет изменения собственного сообщения.
	mux.HandleFunc("POST /chats/{id}/messages/{messageID}/edit", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		editMessage(w, r, store, viewer.Username)
	}))

	// Удаляет собственное сообщение у обоих участников.
	mux.HandleFunc("POST /chats/{id}/messages/{messageID}/delete", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		id, messageID, ok := chatMessagePath(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		err := store.DeleteChatMessage(r.Context(), viewer.Username, id, messageID)
		switch {
		case errors.Is(err, postgres.ErrMessageUnavailable):
			http.NotFound(w, r)
		case err != nil:
			log.Printf("Удаление сообщения: %v", err)
			renderConversation(w, r, store, viewer.Username, 0, http.StatusInternalServerError, "", "Не удалось удалить сообщение.")
		default:
			http.Redirect(w, r, "/chats/"+id, http.StatusSeeOther)
		}
	}))

	// Показывает или скачивает вложение из доступного участнику чата.
	mux.HandleFunc("GET /chats/{id}/messages/{messageID}/attachment", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		id, messageID, ok := chatMessagePath(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		attachment, content, err := store.OpenMessageAttachment(r.Context(), viewer.Username, id, messageID)
		serveChatAttachment(w, r, attachment, content, err)
	}))
}

// sendMessage читает поля формы и сохраняет сообщение с возможным вложением.
func sendMessage(w http.ResponseWriter, r *http.Request, store *postgres.Store, username string) {
	id := r.PathValue("id")
	if !publicProfileID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatAttachmentBytes+(64<<10))
	if err := r.ParseMultipartForm(2 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		renderConversation(w, r, store, username, 0, http.StatusBadRequest, "", "Не удалось прочитать сообщение. Прикрепите один файл до 20 МБ.")
		return
	}

	body := strings.TrimSpace(r.PostForm.Get("message"))
	nonce := r.PostForm.Get("nonce")
	if !messageNonce.MatchString(nonce) {
		renderConversation(w, r, store, username, 0, http.StatusBadRequest, body, "Обновите страницу и повторите отправку.")
		return
	}
	voice := r.PostForm.Get("voice") == "yes"
	var attachment postgres.ChatAttachment
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
		files := r.MultipartForm.File["attachment"]
		if len(files) > 1 {
			renderConversation(w, r, store, username, 0, http.StatusBadRequest, body, "К сообщению можно прикрепить один файл.")
			return
		}
		if len(files) == 1 {
			var err error
			attachment, err = readChatAttachment(files[0], voice)
			if err != nil {
				renderConversation(w, r, store, username, 0, http.StatusBadRequest, body, err.Error())
				return
			}
		}
	}

	if !validProfileText(body, maxMessageLength) || (body == "" && attachment.MIME == "") ||
		(voice && !attachment.Voice) {
		renderConversation(w, r, store, username, 0, http.StatusBadRequest, body, "Введите сообщение до 2000 символов или прикрепите файл.")
		return
	}
	err := store.SendChatMessage(r.Context(), username, id, nonce, body, attachment)
	switch {
	case errors.Is(err, postgres.ErrChatUnavailable):
		http.NotFound(w, r)
	case err != nil:
		log.Printf("Отправка сообщения: %v", err)
		renderConversation(w, r, store, username, 0, http.StatusInternalServerError, body, "Не удалось отправить сообщение.")
	default:
		http.Redirect(w, r, "/chats/"+id, http.StatusSeeOther)
	}
}

// editMessage открывает форму или сохраняет новый текст сообщения владельца.
func editMessage(w http.ResponseWriter, r *http.Request, store *postgres.Store, username string) {
	id, messageID, ok := chatMessagePath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	message, err := store.EditableChatMessage(r.Context(), username, id, messageID)
	switch {
	case errors.Is(err, postgres.ErrMessageUnavailable):
		http.NotFound(w, r)
		return
	case err != nil:
		log.Printf("Загрузка сообщения: %v", err)
		http.Error(w, "Сообщение временно недоступно.", http.StatusInternalServerError)
		return
	}

	page := messageEditPage{PartnerID: id, Message: message}
	if r.Method != http.MethodPost {
		renderPage(w, "message_edit.html", http.StatusOK, page)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil {
		page.Notice = "Не удалось прочитать изменения."
		renderPage(w, "message_edit.html", http.StatusBadRequest, page)
		return
	}
	body := strings.TrimSpace(r.PostForm.Get("message"))
	page.Message.Body = body
	if !validProfileText(body, maxMessageLength) || (body == "" && message.Attachment.MIME == "") {
		page.Notice = "Введите сообщение длиной от 1 до 2000 символов. Для вложения подпись можно оставить пустой."
		renderPage(w, "message_edit.html", http.StatusBadRequest, page)
		return
	}
	err = store.EditChatMessage(r.Context(), username, id, messageID, body)
	switch {
	case errors.Is(err, postgres.ErrMessageUnavailable):
		http.NotFound(w, r)
	case err != nil:
		log.Printf("Редактирование сообщения: %v", err)
		page.Notice = "Не удалось сохранить изменения."
		renderPage(w, "message_edit.html", http.StatusInternalServerError, page)
	default:
		http.Redirect(w, r, "/chats/"+id, http.StatusSeeOther)
	}
}

// chatMessagePath проверяет идентификаторы чата и сообщения из маршрута.
func chatMessagePath(r *http.Request) (string, int64, bool) {
	id := r.PathValue("id")
	messageID, err := strconv.ParseInt(r.PathValue("messageID"), 10, 64)
	return id, messageID, publicProfileID.MatchString(id) && err == nil && messageID > 0
}
