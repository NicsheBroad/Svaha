package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"svaha/internal/web"
)

// main читает адрес прослушивания и запускает веб-сервер.
func main() {
	listenAddress := flag.String("addr", "127.0.0.1:8080", "адрес веб-сервера")
	flag.Parse()

	server := &http.Server{
		Addr:              *listenAddress,
		Handler:           web.NewHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Страница входа доступна на http://%s", *listenAddress)
	log.Fatal(server.ListenAndServe())
}
