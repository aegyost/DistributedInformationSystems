package worker

import (
	"bytes"
	"encoding/json"
	"log"
	"fmt"
	"net/http"
	"time"
	"crackhash/internal/combinations"
	"crackhash/internal/models"
)

type Worker struct {
	managerURL string
	httpClient *http.Client
}

func NewWorker(managerURL string) *Worker {
	return &Worker{
		managerURL: managerURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (w *Worker) HandleTask(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { 
		http.Error(rw, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var task models.WorkerTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil { 
		http.Error(rw, "Invalid JSON", http.StatusBadRequest)
		return
	}

	go w.processTask(task)

	rw.WriteHeader(http.StatusAccepted)
}

func (w *Worker) processTask(task models.WorkerTaskRequest) {
	log.Printf("Processing task: part %d/%d, hash=%s", task.PartNumber, task.PartCount, task.Hash)

	total, err := combinations.TotalCombinations(task.Alphabet, task.MaxLength)
	if err != nil {
		log.Printf("Error calculating total combinations: %v", err)
		return
	}
	if total == 0 {
		log.Println("No combinations to process")
		return
	}

	partSize := total / uint64(task.PartCount) 
	remainder := total % uint64(task.PartCount) 

	start := uint64(task.PartNumber-1) * partSize 
	if uint64(task.PartNumber-1) < remainder {
		start += uint64(task.PartNumber - 1)
	} else {
		start += remainder
	}
	end := start + partSize 
	if uint64(task.PartNumber) <= remainder {
		end++
	}
	if end > total {
		end = total
	}

	log.Printf("Index range for part %d: [%d, %d)", task.PartNumber, start, end)

	var found []string 
	for idx := start; idx < end; idx++ {
		word, err := combinations.WordByIndex(idx, task.Alphabet, task.MaxLength) 
		if err != nil {
			log.Printf("Error getting word for index %d: %v", idx, err)
			continue
		}
		if combinations.CheckHash(word, task.Hash) { 
			found = append(found, word) 
			log.Printf("Found match: %s", word)
		}
	}

	response := models.WorkerResponse{
		RequestID: task.RequestID,
		Words:     found,
	}
	if err := w.sendResponse(response); err != nil { 
		log.Printf("Failed to send response to manager: %v", err)
	}
}

func (w *Worker) sendResponse(resp models.WorkerResponse) error { 
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	url := w.managerURL + "/internal/api/manager/hash/crack/request"
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := w.httpClient.Do(req) 
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("Unexpected status: %d", res.StatusCode)
	}
	return nil
}