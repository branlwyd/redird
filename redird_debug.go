package main

import (
	"log"
	"net/http"
)

func serve(_, _, _ string, h http.Handler) {
	log.Printf("Serving debug on :8080")
	log.Fatalf("ListenAndServe: %v", http.ListenAndServe(":8080", h))
}
