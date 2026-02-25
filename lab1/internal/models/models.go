package models

type CrackHashRequest struct {
	Hash      string `json:"hash"`
	MaxLength int    `json:"maxLength"`
}

type CrackHashResponse struct {
	RequestID string `json:"requestId"`
}

type StatusResponse struct {
	Status string   `json:"status"`
	Data   []string `json:"data"`
}

type WorkerTaskRequest struct {
	RequestID  string `json:"requestId"` 
	Hash       string `json:"hash"`
	MaxLength  int    `json:"maxLength"`
	Alphabet   string `json:"alphabet"`
	PartNumber int    `json:"partNumber"`
	PartCount  int    `json:"partCount"`
}

type WorkerResponse struct {
	RequestID string   `json:"requestId"`
	Words     []string `json:"words"`
}