package main

import (
    "log"
    "os"

    "crackhash/internal/worker"
)

func main() {
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

    workerID := os.Getenv("WORKER_ID")
    if workerID == "" {
        workerID = "worker-unknown"
    }

    if err := worker.RunWorker(rabbitURI, tasksQueue, resultsQueue, workerID); err != nil {
        log.Fatalf("Worker failed: %v", err)
    }
}