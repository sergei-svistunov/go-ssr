package main

import (
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web"
)

func main() {
	// CSRF_KEY is 64 hex characters, such as the output of `openssl rand -hex 32`.
	csrfKey, err := hex.DecodeString(os.Getenv("CSRF_KEY"))
	if err != nil || len(csrfKey) != 32 {
		log.Fatal("CSRF_KEY must be 64 hex characters; generate one with: openssl rand -hex 32")
	}

	handler, err := web.New(&model.Model{}, csrfKey)
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{Addr: ":18080", Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Println("Listening on :18080")
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
