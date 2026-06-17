package main

import (
	"log"
	"net/http"
	"os"

	"bpdrive/internal/app"
)

func main() {
	addr := getenv("BPDRIVE_ADDR", ":8088")
	dataDir := getenv("BPDRIVE_DATA", "./data")

	srv, err := app.New(dataDir)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("bpdrive listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
