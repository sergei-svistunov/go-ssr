package main

import (
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"time"

	"<PKG_NAME>/<WEB_DIR>"
)

func main() {
	// CSRF_KEY comes from your secret store: 64 hex characters, such as the
	// output of `openssl rand -hex 32`.
	csrfKey, err := hex.DecodeString(os.Getenv("CSRF_KEY"))
	if err != nil || len(csrfKey) != 32 {
		log.Fatal("CSRF_KEY must be 64 hex characters; generate one with: openssl rand -hex 32")
	}

	handler, err := web.New(csrfKey)
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{Addr: ":8080", Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Print("Server listens on http://localhost:8080/")
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
