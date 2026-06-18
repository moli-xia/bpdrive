package main

import (
	"log"
	"net/http"
	"os"

	"dpdrive/internal/app"
)

func main() {
	addr := getenv("DPDRIVE_ADDR", getenv("BPDRIVE_ADDR", ":8088"))
	dataDir := getenv("DPDRIVE_DATA", getenv("BPDRIVE_DATA", "./data"))

	srv, err := app.New(dataDir)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("dpdrive listening on %s", addr)
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
