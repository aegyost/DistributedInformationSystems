package manager

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
	"crackhash/internal/models"
	"github.com/google/uuid"
)

const (
	StatusInProgress = "IN_PROGRESS"
	StatusReady      = "READY"
	StatusError      = "ERROR"
)

type RequestState struct {
	Status            string
	Data              []string
	ExpectedResponses int
	ReceivedResponses int
	CreatedAt         time.Time
	TimeoutCancel     func()
	mu                sync.Mutex
}

type Manager struct {
	workers    []string 
	requests   map[string]*RequestState
	mu         sync.RWMutex
	httpClient *http.Client 
	timeout    time.Duration 
}

func NewManager(workers []string, timeout time.Duration) *Manager {
	return &Manager{
		workers:    workers,
		requests:   make(map[string]*RequestState),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		timeout:    timeout,
	}
}

func (m *Manager) HandleCrack(w http.ResponseWriter, r *http.Request) { 
	if r.Method != http.MethodPost { 
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req models.CrackHashRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { 
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.MaxLength <= 0 { 
		http.Error(w, "maxLength must be positive", http.StatusBadRequest)
		return
	}

	requestID := uuid.New().String() 

	state := &RequestState{
		Status:            StatusInProgress,
		Data:              nil,
		ExpectedResponses: len(m.workers),
		ReceivedResponses: 0,
		CreatedAt:         time.Now(), 
	}

	m.mu.Lock() 
	m.requests[requestID] = state
	m.mu.Unlock()

	timer := time.AfterFunc(m.timeout, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if s, ok := m.requests[requestID]; ok && s.Status == StatusInProgress {
			s.Status = StatusError
			log.Printf("Request %s timed out", requestID)
		}
	})
	state.TimeoutCancel = func() { timer.Stop() }

	alphabet := "abcdefghijklmnopqrstuvwxyz0123456789"
	partCount := len(m.workers)

	for i, workerURL := range m.workers {
		partNumber := i + 1
		task := models.WorkerTaskRequest{
			RequestID:  requestID,
			Hash:       req.Hash,
			MaxLength:  req.MaxLength,
			Alphabet:   alphabet,
			PartNumber: partNumber,
			PartCount:  partCount,
		}

		go func(url string, task models.WorkerTaskRequest) {
			if err := m.sendTaskToWorker(url, task); err != nil {
				log.Printf("Failed to send task to worker %s: %v", url, err)
			}
		}(workerURL, task)
	}

	resp := models.CrackHashResponse{RequestID: requestID}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp) 
}

func (m *Manager) sendTaskToWorker(workerURL string, task models.WorkerTaskRequest) error {
	data, err := json.Marshal(task) 
	if err != nil {
		return err
	}
	url := workerURL + "/internal/api/worker/hash/crack/task"
	resp, err := m.httpClient.Post(url, "application/json", bytes.NewReader(data)) 
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted { 
		return fmt.Errorf("worker returned status %d", resp.StatusCode)
	}
	return nil
}

func (m *Manager) HandleWorkerResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch { 
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var resp models.WorkerResponse 
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil { 
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	m.mu.Lock() 
	defer m.mu.Unlock()

	state, exists := m.requests[resp.RequestID] 
	if !exists { 
		http.Error(w, "Request not found", http.StatusNotFound)
		return
	}

	state.mu.Lock() 
	defer state.mu.Unlock()

	if state.Status != StatusInProgress { 
		w.WriteHeader(http.StatusOK)
		return
	}

	state.Data = append(state.Data, resp.Words...) 
	state.ReceivedResponses++ 

	if state.ReceivedResponses >= state.ExpectedResponses { 
		state.Status = StatusReady 
		if state.TimeoutCancel != nil { 
			state.TimeoutCancel()
		}
		log.Printf("Request %s completed with %d words", resp.RequestID, len(state.Data))
	}

	w.WriteHeader(http.StatusOK)
}

func (m *Manager) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet { 
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestID := r.URL.Query().Get("requestId") 
	if requestID == "" {
		http.Error(w, "Missing requestId", http.StatusBadRequest)
		return
	}

	m.mu.RLock() 
	state, exists := m.requests[requestID]
	m.mu.RUnlock()

	if !exists { 
		http.Error(w, "Request not found", http.StatusNotFound)
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	resp := models.StatusResponse{ 
		Status: state.Status,
		Data:   state.Data,
	}
	if state.Status != StatusReady {
		resp.Data = nil
	}

	w.Header().Set("Content-Type", "application/json") 
	json.NewEncoder(w).Encode(resp)
}