package web

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"svaha/internal/postgres"
)

type searchPage struct {
	LookingFor  string
	City        string
	Profile     *postgres.SearchProfile
	Initial     string
	ReactionURL string
	Notice      string
}

// registerSearchRoute добавляет просмотр и оценку анкет для пользователей с активной сессией.
func registerSearchRoute(mux *http.ServeMux, store *postgres.Store) {
	// Показывает следующую анкету по выбранным фильтрам.
	mux.HandleFunc("GET /search", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		page, gender := readSearchFilters(r.URL.Query(), viewer.LookingFor)
		if page.Notice != "" {
			renderPage(w, "search.html", http.StatusBadRequest, page)
			return
		}
		profile, err := store.NextSearchProfile(r.Context(), viewer.Username, gender, page.City)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			log.Printf("Поиск анкет: %v", err)
			page.Notice = "Поиск временно недоступен."
			renderPage(w, "search.html", http.StatusInternalServerError, page)
			return
		default:
			page.Profile = &profile
			initial, _ := utf8.DecodeRuneInString(profile.FirstName)
			page.Initial = strings.ToUpper(string(initial))
			page.ReactionURL = searchURL("/search/reaction", page)
		}
		renderPage(w, "search.html", http.StatusOK, page)
	}))

	// Сохраняет реакцию и возвращает пользователя к поиску с теми же фильтрами.
	mux.HandleFunc("POST /search/reaction", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Не удалось прочитать реакцию.", http.StatusBadRequest)
			return
		}
		id, choice := r.PostForm.Get("profile_id"), r.PostForm.Get("choice")
		if !publicProfileID.MatchString(id) || (choice != "like" && choice != "pass") {
			http.Error(w, "Неверная реакция на анкету.", http.StatusBadRequest)
			return
		}
		if err := store.RecordSearchReaction(r.Context(), viewer.Username, id, choice == "like"); err != nil {
			log.Printf("Сохранение реакции на анкету: %v", err)
			http.Error(w, "Не удалось сохранить реакцию. Попробуйте ещё раз.", http.StatusInternalServerError)
			return
		}
		page, _ := readSearchFilters(r.URL.Query(), viewer.LookingFor)
		http.Redirect(w, r, searchURL("/search", page), http.StatusSeeOther)
	}))
}

// readSearchFilters читает параметры поиска и сопоставляет предпочтение с полем анкеты.
func readSearchFilters(query url.Values, preference string) (searchPage, string) {
	lookingFor := query.Get("looking_for")
	if !query.Has("looking_for") {
		lookingFor = preference
	}
	gender := ""
	switch lookingFor {
	case "women":
		gender = "female"
	case "men":
		gender = "male"
	default:
		lookingFor = "anyone"
	}
	page := searchPage{LookingFor: lookingFor, City: strings.TrimSpace(query.Get("city"))}
	if !utf8.ValidString(page.City) || utf8.RuneCountInString(page.City) > 100 || strings.ContainsRune(page.City, 0) {
		page.City = ""
		page.Notice = "Введите название города длиной до 100 символов."
	}
	return page, gender
}

// searchURL сохраняет фильтры при отправке реакции и переходе к следующей анкете.
func searchURL(path string, page searchPage) string {
	values := url.Values{"looking_for": {page.LookingFor}}
	if page.City != "" {
		values.Set("city", page.City)
	}
	return path + "?" + values.Encode()
}
