package main

import (
	"log"
	"net/http"
	"os"
	"crackhash/internal/worker"
)

func main() {
	managerURL := os.Getenv("MANAGER_URL")
	if managerURL == "" {
		log.Fatal("MANAGER_URL not set")
	}

	w := worker.NewWorker(managerURL)

	http.HandleFunc("/internal/api/worker/hash/crack/task", w.HandleTask) 

	log.Println("Worker started on :8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}