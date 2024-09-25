package main

import (
	"log"
	"net/http"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web"
)

func main() {
	if err := http.ListenAndServe(":18080", web.New(&model.Model{})); err != nil {
		log.Fatal(err)
	}
}
