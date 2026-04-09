package models

import "time"

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

type RequestDocument struct {
	ID             string    `bson:"_id"`
	Hash           string    `bson:"hash"`
	MaxLength      int       `bson:"max_length"`
	Status         string    `bson:"status"`
	CreatedAt      time.Time `bson:"created_at"`
	UpdatedAt      time.Time `bson:"updated_at"`
	WordsFound     []string  `bson:"words_found"`
	TasksSent      int       `bson:"tasks_sent"`
	TasksExpected  int       `bson:"tasks_expected"`
	TasksReceived  int       `bson:"tasks_received"`
}

type OutboxDocument struct {
	ID        string              `bson:"_id"`
	RequestID string              `bson:"request_id"`
	Tasks     []WorkerTaskRequest `bson:"tasks"`
	CreatedAt time.Time           `bson:"created_at"`
}