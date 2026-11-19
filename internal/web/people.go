package web

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"svaha/internal/postgres"
)

var publicProfileID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type publicProfilePage struct {
	postgres.PublicProfile
	BackURL    string
	BackLabel  string
	Navigation string
}

// registerPeopleRoutes подключает входящие лайки, просмотр анкет и их фотографий.
func registerPeopleRoutes(mux *http.ServeMux, store *postgres.Store) {
	// Показывает входящие лайки от людей без начатой переписки.
	mux.HandleFunc("GET /likes", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		profiles, err := store.IncomingLikes(r.Context(), viewer.Username)
		if err != nil {
			log.Printf("Загрузка входящих лайков: %v", err)
			http.Error(w, "Входящие лайки временно недоступны.", http.StatusInternalServerError)
			return
		}
		renderPage(w, "likes.html", http.StatusOK, profiles)
	}))

	// Показывает анкету другого пользователя без его контактных данных.
	mux.HandleFunc("GET /people/{id}", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		id := r.PathValue("id")
		if !publicProfileID.MatchString(id) {
			http.NotFound(w, r)
			return
		}
		profile, err := store.PublicProfile(r.Context(), viewer.Username, id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			http.NotFound(w, r)
		case err != nil:
			log.Printf("Загрузка анкеты: %v", err)
			http.Error(w, "Анкета временно недоступна.", http.StatusInternalServerError)
		default:
			renderPage(w, "person.html", http.StatusOK, personPage(profile, r.URL.Query(), viewer.LookingFor))
		}
	}))

	// Отдаёт фото из просматриваемой анкеты вошедшему пользователю.
	mux.HandleFunc("GET /people/{id}/photo/{photoID}", withSession(store, func(w http.ResponseWriter, r *http.Request, viewer postgres.Viewer) {
		id := r.PathValue("id")
		photoID, err := strconv.ParseInt(r.PathValue("photoID"), 10, 64)
		if !publicProfileID.MatchString(id) || err != nil || photoID < 1 {
			http.NotFound(w, r)
			return
		}
		photo, err := store.PublicProfilePhoto(r.Context(), viewer.Username, id, photoID)
		serveProfilePhoto(w, r, photo, err)
	}))
}

// personPage подбирает ссылку возврата и активную вкладку по источнику перехода.
func personPage(profile postgres.PublicProfile, query url.Values, preference string) publicProfilePage {
	page := publicProfilePage{PublicProfile: profile}
	switch query.Get("from") {
	case "chat":
		page.BackURL = "/chats/" + profile.ID
		page.BackLabel = "Вернуться к чату"
		page.Navigation = "chat"
	case "likes":
		page.BackURL = "/likes"
		page.BackLabel = "Вернуться к входящим лайкам"
		page.Navigation = "liked-person"
	default:
		filters, _ := readSearchFilters(query, preference)
		page.BackURL = searchURL("/search", filters)
		page.BackLabel = "Вернуться к поиску"
		page.Navigation = "people"
	}
	return page
}
