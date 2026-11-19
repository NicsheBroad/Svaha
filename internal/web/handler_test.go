package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"svaha/internal/postgres"
)

// TestRoutes проверяет регистрацию маршрутов и доступ к страницам без сессии.
func TestRoutes(t *testing.T) {
	handler := NewHandler(nil)
	for _, path := range []string{"/", "/register"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Errorf("%s: получен статус %d", path, response.Code)
		}
	}
	for _, path := range []string{"/profile", "/profile/edit", "/profile/blocks", "/search", "/likes", "/chats", "/chats/test", "/chats/test?view=history", "/chats/test/messages/1/edit", "/chats/test/messages/1/attachment", "/people/test", "/people/test/photo/1"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/" {
			t.Errorf("%s: ожидалось перенаправление на вход, получен статус %d", path, response.Code)
		}
	}
}

// TestInvalidLogin проверяет, что недопустимые данные входа отклоняются до обращения к базе.
func TestInvalidLogin(t *testing.T) {
	handler := NewHandler(nil)
	for _, form := range []string{"username=bad%00login&password=example", "username=root&password="} {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("ожидался отказ во входе, получен статус %d", response.Code)
		}
	}
}

// TestChatHistoryFragment проверяет, что история рендерится отдельно от всей формы чата.
func TestChatHistoryFragment(t *testing.T) {
	const id = "59704cf5-979e-4c2b-bca4-7db32ac5a168"
	page := chatPage{
		Conversation: postgres.Conversation{
			Partner:  postgres.ChatPartner{ID: id},
			Messages: []postgres.ChatMessage{{ID: 3, Body: "<сообщение>"}},
		},
		Latest: true,
	}
	var output bytes.Buffer
	if err := pageTemplates.ExecuteTemplate(&output, "chatHistory", page); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	if !strings.Contains(html, `id="chat-history"`) || !strings.Contains(html, "&lt;сообщение&gt;") ||
		strings.Contains(html, `id="chat-form"`) || strings.Contains(html, "<script>") {
		t.Fatal("фрагмент должен содержать только размеченную историю сообщений")
	}
}

// TestDeleteChatRoute проверяет, что удаление требует POST и действующую сессию.
func TestDeleteChatRoute(t *testing.T) {
	handler := NewHandler(nil)
	const path = "/chats/59704cf5-979e-4c2b-bca4-7db32ac5a168/delete"

	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, path, nil))
	if get.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: ожидался статус 405, получен %d", get.Code)
	}

	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, path, nil))
	if post.Code != http.StatusSeeOther || post.Header().Get("Location") != "/" {
		t.Errorf("POST без сессии: ожидалось перенаправление на вход, получен статус %d", post.Code)
	}
}

// TestMessageActions проверяет, что без сессии нельзя править или удалять сообщения.
func TestMessageActions(t *testing.T) {
	handler := NewHandler(nil)
	for _, action := range []string{"edit", "delete"} {
		path := "/chats/59704cf5-979e-4c2b-bca4-7db32ac5a168/messages/1/" + action
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/" {
			t.Errorf("%s: ожидалось перенаправление на вход, получен статус %d", action, response.Code)
		}
	}
}

// TestSafetyActionsRequireSession проверяет, что жалобы и блокировки доступны только после входа.
func TestSafetyActionsRequireSession(t *testing.T) {
	handler := NewHandler(nil)
	const id = "59704cf5-979e-4c2b-bca4-7db32ac5a168"
	for _, path := range []string{"/people/" + id + "/block", "/people/" + id + "/report", "/profile/blocks/" + id + "/unblock", "/likes/" + id + "/reply"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/" {
			t.Errorf("%s: ожидалось перенаправление на вход, получен статус %d", path, response.Code)
		}
	}
}

// TestChatMediaRendering проверяет отображение вложений и доступность действий только для своих сообщений.
func TestChatMediaRendering(t *testing.T) {
	const id = "59704cf5-979e-4c2b-bca4-7db32ac5a168"
	page := chatPage{Conversation: postgres.Conversation{
		Partner: postgres.ChatPartner{ID: id},
		Messages: []postgres.ChatMessage{
			{ID: 1, Own: true, Edited: true, Attachment: postgres.ChatAttachment{Name: "голос.webm", MIME: "audio/webm", Voice: true}},
			{ID: 2, Attachment: postgres.ChatAttachment{Name: "видео.mp4", MIME: "video/mp4"}},
			{ID: 3, Attachment: postgres.ChatAttachment{Name: "фото.jpg", MIME: "image/jpeg"}},
		},
	}}
	var output bytes.Buffer
	if err := pageTemplates.ExecuteTemplate(&output, "chat.html", page); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, snippet := range []string{"<audio src=", "<video src=", "<img src=", "изменено", "Записать голос",
		`/messages/1/edit"`, `/messages/1/delete"`, `enctype="multipart/form-data"`} {
		if !strings.Contains(html, snippet) {
			t.Errorf("в чате отсутствует %q", snippet)
		}
	}
	if strings.Contains(html, "/messages/2/edit") || strings.Contains(html, "/messages/2/delete") {
		t.Error("в чужом сообщении есть действия автора")
	}
}

// TestSignedInPages проверяет рендеринг личных страниц и навигацию между ними.
func TestSignedInPages(t *testing.T) {
	const id = "59704cf5-979e-4c2b-bca4-7db32ac5a168"
	candidate := postgres.SearchProfile{ID: id, FirstName: "Анна"}
	pages := []struct {
		name     string
		data     any
		snippets []string
	}{
		{"search.html", searchPage{Profile: &candidate, LookingFor: "women"},
			[]string{`id="search-card-link" href="/people/` + id, `class="swipe-card"`, `value="pass"`, `value="like"`}},
		{"profile.html", profilePage{User: postgres.User{FirstName: "Анна"}},
			[]string{`href="/profile" aria-current="page"`, `href="/profile/edit"`, `href="/profile/blocks"`}},
		{"profile_edit.html", profileEditPage{MaxPhotos: postgres.MaxProfilePhotos},
			[]string{`action="/profile/edit"`, "Сохранить изменения"}},
		{"chats.html", []postgres.ChatPartner{{ID: id, FirstName: "Анна", Unread: 2}},
			[]string{`href="/likes"`, `href="/chats" aria-current="page"`, "Непрочитано: 2"}},
		{"likes.html", incomingLikesPage{Profiles: []postgres.SearchProfile{candidate}, MatchID: id},
			[]string{`href="/likes" aria-current="page"`, `href="/people/` + id + `?from=likes"`, `action="/likes/` + id + `/reply"`, "Открыть чат"}},
		{"chat.html", chatPage{Conversation: postgres.Conversation{Partner: postgres.ChatPartner{ID: id, FirstName: "Анна"}}},
			[]string{`href="/people/` + id + `?from=chat"`, `action="/chats/` + id + `/delete"`, "Подтвердить удаление"}},
		{"message_edit.html", messageEditPage{PartnerID: id, Message: postgres.ChatMessage{ID: 1, Body: "Привет"}},
			[]string{`/messages/1/edit"`, "Привет"}},
		{"person.html", personPage(postgres.PublicProfile{ID: id, FirstName: "Анна"}, url.Values{"from": {"chat"}}, ""),
			[]string{`href="/chats/` + id + `"`, "Вернуться к чату", `href="/chats" aria-current="true"`,
				`action="/people/` + id + `/block"`, `action="/people/` + id + `/report"`}},
		{"person.html", personPage(postgres.PublicProfile{ID: id, FirstName: "Анна"}, url.Values{"from": {"likes"}}, ""),
			[]string{`href="/likes" aria-current="true"`, "Вернуться к входящим лайкам"}},
	}
	for _, page := range pages {
		var result bytes.Buffer
		if err := pageTemplates.ExecuteTemplate(&result, page.name, page.data); err != nil {
			t.Fatal(err)
		}
		for _, snippet := range page.snippets {
			if !strings.Contains(result.String(), snippet) {
				t.Errorf("%s: отсутствует %q", page.name, snippet)
			}
		}
	}
}

// TestNavigationOrder проверяет порядок вкладок в общей верхней панели.
func TestNavigationOrder(t *testing.T) {
	var output bytes.Buffer
	if err := pageTemplates.ExecuteTemplate(&output, "mainNavigation", "likes"); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	previous := -1
	for _, path := range []string{"/search", "/likes", "/chats", "/profile"} {
		current := strings.Index(html, `href="`+path+`"`)
		if current <= previous {
			t.Fatalf("вкладка %s отсутствует или расположена неверно", path)
		}
		previous = current
	}
}
