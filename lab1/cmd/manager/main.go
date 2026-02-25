package main

import (
	"embed"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"crackhash/internal/manager"
)

//go:embed web/index.html
var webUI embed.FS

func main() {
	workersEnv := os.Getenv("WORKER_URLS")
	if workersEnv == "" {
		log.Fatal("WORKER_URLS not set")
	}
	workers := strings.Split(workersEnv, ",")

	timeout := 1 * time.Minute

	mgr := manager.NewManager(workers, timeout)

	log.Println("Manager started on :8080")
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
    	if r.URL.Path != "/" {
        	http.NotFound(w, r)
        	return
    	}
    	data, err := webUI.ReadFile("web/index.html")
    	if err != nil {
        	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
        	return
    	}
    	w.Header().Set("Content-Type", "text/html")
    	w.Write(data)
	})

	http.HandleFunc("/api/hash/crack", mgr.HandleCrack)
	http.HandleFunc("/internal/api/manager/hash/crack/request", mgr.HandleWorkerResponse)
	http.HandleFunc("/api/hash/status", mgr.HandleStatus)

	
	log.Fatal(http.ListenAndServe(":8080", nil))
}