package web

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*.html
var templateFiles embed.FS

var pageTemplates = template.Must(template.ParseFS(templateFiles, "templates/*.html"))

// NewHandler регистрирует маршруты входа и регистрации.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	// Показывает форму входа.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		renderPage(w, "login.html", http.StatusOK, "")
	})
	// Сообщает, что вход пока недоступен.
	mux.HandleFunc("POST /{$}", func(w http.ResponseWriter, _ *http.Request) {
		renderPage(w, "login.html", http.StatusNotImplemented, "Вход пока недоступен.")
	})
	// Показывает форму регистрации.
	mux.HandleFunc("GET /register", func(w http.ResponseWriter, _ *http.Request) {
		renderPage(w, "register.html", http.StatusOK, "")
	})
	// Сообщает, что регистрация пока недоступна.
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, _ *http.Request) {
		renderPage(w, "register.html", http.StatusNotImplemented, "Регистрация пока недоступна.")
	})
	return mux
}

// renderPage отправляет HTML-страницу с заданным статусом и сообщением.
func renderPage(w http.ResponseWriter, templateName string, status int, notice string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = pageTemplates.ExecuteTemplate(w, templateName, notice)
}
