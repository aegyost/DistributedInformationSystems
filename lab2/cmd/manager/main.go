package main

import (
    "embed"
    "log"
    "net/http"
    "os"
    "time"
    "strconv"

    "crackhash/internal/manager"
)

//go:embed web/index.html
var webUI embed.FS

func main() {
    mongoURI := os.Getenv("MONGO_URI")
    if mongoURI == "" {
        log.Fatal("MONGO_URI not set")
    }

    rabbitURI := os.Getenv("RABBITMQ_URI")
    if rabbitURI == "" {
        log.Fatal("RABBITMQ_URI not set")
    }

    tasksQueue := os.Getenv("TASKS_QUEUE")
    if tasksQueue == "" {
        tasksQueue = "crack_tasks"
    }

    resultsQueue := os.Getenv("RESULTS_QUEUE")
    if resultsQueue == "" {
        resultsQueue = "crack_results"
    }

    workersCount := 2
    if wcStr := os.Getenv("WORKERS_COUNT"); wcStr != "" {
        wc, err := strconv.Atoi(wcStr)
        if err == nil && wc > 0 {
            workersCount = wc
        } else {
            log.Printf("Invalid WORKERS_COUNT value '%s', using default %d", wcStr, workersCount)
        }
    }

    timeout := 1 * time.Minute

    mgr, err := manager.NewManager(mongoURI, rabbitURI, tasksQueue, resultsQueue, workersCount, timeout)
    if err != nil {
        log.Fatalf("Failed to create manager: %v", err)
    }
    defer mgr.Close()

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
    http.HandleFunc("/api/hash/status", mgr.HandleStatus)

    log.Println("Manager started on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}