package worker

import (
    "encoding/json"
    "fmt"
    "log"
    "time"

    "crackhash/internal/combinations"
    "crackhash/internal/models"

    amqp "github.com/streadway/amqp"
)

type Worker struct {
    conn          *amqp.Connection
    channel       *amqp.Channel
    tasksQueue    string
    resultsQueue  string
    workerID      string
    stopChan      chan bool
}

func NewWorker(rabbitURI, tasksQueue, resultsQueue, workerID string) (*Worker, error) {
    conn, err := amqp.Dial(rabbitURI)
    if err != nil {
        return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
    }

    ch, err := conn.Channel()
    if err != nil {
        return nil, fmt.Errorf("failed to open channel: %w", err)
    }

    err = ch.Qos(1, 0, false)
    if err != nil {
        return nil, fmt.Errorf("failed to set QoS: %w", err)
    }

    _, err = ch.QueueDeclare(tasksQueue, true, false, false, false, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to declare tasks queue: %w", err)
    }
    _, err = ch.QueueDeclare(resultsQueue, true, false, false, false, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to declare results queue: %w", err)
    }

    return &Worker{
        conn:         conn,
        channel:      ch,
        tasksQueue:   tasksQueue,
        resultsQueue: resultsQueue,
        workerID:     workerID,
        stopChan:     make(chan bool),
    }, nil
}

func (w *Worker) Run() error {
    msgs, err := w.channel.Consume(
        w.tasksQueue,
        w.workerID,
        false,
        false,
        false,
        false,
        nil,
    )
    if err != nil {
        return fmt.Errorf("failed to consume tasks: %w", err)
    }

    log.Printf("Worker %s started, waiting for tasks...", w.workerID)

    for {
        select {
        case <-w.stopChan:
            log.Printf("Worker %s stopping", w.workerID)
            return nil
        case d := <-msgs:
            go w.processTask(d)
        }
    }
}

func (w *Worker) processTask(d amqp.Delivery) {
    var task models.WorkerTaskRequest
    if err := json.Unmarshal(d.Body, &task); err != nil {
        log.Printf("Worker %s: failed to unmarshal task: %v", w.workerID, err)
        d.Nack(false, false)
        return
    }

    log.Printf("Worker %s: processing task part %d/%d, hash=%s",
        w.workerID, task.PartNumber, task.PartCount, task.Hash)

    total, err := combinations.TotalCombinations(task.Alphabet, task.MaxLength)
    if err != nil {
        log.Printf("Worker %s: error calculating total: %v", w.workerID, err)
        d.Nack(false, true)
        return
    }
    if total == 0 {
        log.Printf("Worker %s: no combinations to process", w.workerID)
        d.Ack(false)
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

    log.Printf("Worker %s: index range [%d, %d)", w.workerID, start, end)

    var found []string
    for idx := start; idx < end; idx++ {
        word, err := combinations.WordByIndex(idx, task.Alphabet, task.MaxLength)
        if err != nil {
            log.Printf("Worker %s: error getting word for index %d: %v", w.workerID, idx, err)
            continue
        }
        if combinations.CheckHash(word, task.Hash) {
            found = append(found, word)
            log.Printf("Worker %s: found match: %s", w.workerID, word)
        }
    }

    result := models.WorkerResponse{
        RequestID: task.RequestID,
        Words:     found,
    }
    resultBody, err := json.Marshal(result)
    if err != nil {
        log.Printf("Worker %s: failed to marshal result: %v", w.workerID, err)
        d.Nack(false, true)
        return
    }

    err = w.channel.Publish(
        "",
        w.resultsQueue,
        true,
        false,
        amqp.Publishing{
            ContentType:  "application/json",
            Body:         resultBody,
            DeliveryMode: amqp.Persistent,
        })
    if err != nil {
        log.Printf("Worker %s: failed to publish result: %v", w.workerID, err)
        d.Nack(false, true)
        return
    }

    d.Ack(false)
    log.Printf("Worker %s: completed task for request %s", w.workerID, task.RequestID)
}

func (w *Worker) Stop() {
    close(w.stopChan)
    w.channel.Close()
    w.conn.Close()
}

func RunWorker(rabbitURI, tasksQueue, resultsQueue, workerID string) error {
    for {
        w, err := NewWorker(rabbitURI, tasksQueue, resultsQueue, workerID)
        if err != nil {
            log.Printf("Failed to create worker: %v, retrying in 5 seconds...", err)
            time.Sleep(5 * time.Second)
            continue
        }
        err = w.Run()
        if err != nil {
            log.Printf("Worker run error: %v, reconnecting...", err)
            w.Stop()
            time.Sleep(5 * time.Second)
            continue
        }
        return nil
    }
}