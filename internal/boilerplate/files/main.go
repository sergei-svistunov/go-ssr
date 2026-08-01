package main

import (
	"log"
	"net/http"

	"<PKG_NAME>/<WEB_DIR>"
)

func main() {
	log.Print("Server listens on http://localhost:8080/")
	if err := http.ListenAndServe(":8080", web.New()); err != nil {
		log.Fatal(err)
	}
}
