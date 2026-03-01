package main

import (
	"io"
	"log"
	"net/http"
)

func main() {
	log.Println("starting server, listening on port 8090")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%v", r.RequestURI)
		_, _ = io.WriteString(w, "Welcome to home page")
	})
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%v", r.RequestURI)
		_, _ = io.WriteString(w, "pong")
	})
	if err := http.ListenAndServe(":8090", mux); err != nil {
		log.Fatal(err)
	}
}
