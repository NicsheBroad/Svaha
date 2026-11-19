package web

import (
	"bytes"
	"embed"
	"html/template"
	"log"
	"net/http"

	"svaha/internal/postgres"
)

//go:embed templates
var templateFiles embed.FS

var pageTemplates = template.Must(template.New("").Funcs(template.FuncMap{"photoNumber": photoNumber}).ParseFS(templateFiles, "templates/*/*.html"))

// NewHandler подключает маршруты приложения и защищает формы от сторонних запросов.
func NewHandler(store *postgres.Store) http.Handler {
	mux := http.NewServeMux()
	registerAuthRoutes(mux, store)
	registerProfileRoutes(mux, store)
	registerSearchRoute(mux, store)
	registerPeopleRoutes(mux, store)
	registerSafetyRoutes(mux, store)
	registerChatRoutes(mux, store)
	return http.NewCrossOriginProtection().Handler(mux)
}

// renderPage рендерит страницу перед отправкой ответа, чтобы обработать ошибки шаблона.
func renderPage(w http.ResponseWriter, templateName string, status int, data any) {
	var page bytes.Buffer
	if err := pageTemplates.ExecuteTemplate(&page, templateName, data); err != nil {
		log.Printf("Рендеринг %s: %v", templateName, err)
		http.Error(w, "Не удалось открыть страницу.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = page.WriteTo(w)
}
