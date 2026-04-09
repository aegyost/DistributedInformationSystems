package manager

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "sync"
    "time"

    "crackhash/internal/models"

    "github.com/google/uuid"
    amqp "github.com/streadway/amqp"
    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
    "go.mongodb.org/mongo-driver/mongo/readpref"
    "go.mongodb.org/mongo-driver/mongo/writeconcern"
)

const (
    StatusInProgress = "IN_PROGRESS"
    StatusReady      = "READY"
    StatusError      = "ERROR"
)

type Manager struct {
    mongoClient      *mongo.Client
    requestsColl     *mongo.Collection
    outboxColl       *mongo.Collection
    rabbitConn       *amqp.Connection
    rabbitChan       *amqp.Channel
    tasksQueueName   string
    resultsQueueName string
    workersCount     int
    mu               sync.RWMutex
    timeout          time.Duration
    stopChan         chan struct{}
}

func (m *Manager) monitorRabbitMQ(rabbitURI string) {
    for {
        if m.rabbitConn == nil || m.rabbitConn.IsClosed() {
            log.Println("RabbitMQ connection lost, reconnecting...")

            select {
            case <-m.stopChan:
            default:
                close(m.stopChan)
            }

            conn, err := amqp.Dial(rabbitURI)
            if err != nil {
                log.Printf("Failed to reconnect to RabbitMQ: %v, retrying in 5s", err)
                time.Sleep(5 * time.Second)
                continue
            }

            ch, err := conn.Channel()
            if err != nil {
                log.Printf("Failed to open channel: %v", err)
                conn.Close()
                time.Sleep(5 * time.Second)
                continue
            }

            _, err = ch.QueueDeclare(m.tasksQueueName, true, false, false, false, nil)
            if err != nil {
                log.Printf("Failed to declare tasks queue: %v", err)
                conn.Close()
                ch.Close()
                time.Sleep(5 * time.Second)
                continue
            }
            _, err = ch.QueueDeclare(m.resultsQueueName, true, false, false, false, nil)
            if err != nil {
                log.Printf("Failed to declare results queue: %v", err)
                conn.Close()
                ch.Close()
                time.Sleep(5 * time.Second)
                continue
            }

            m.mu.Lock()
            oldConn := m.rabbitConn
            oldChan := m.rabbitChan
            m.rabbitConn = conn
            m.rabbitChan = ch
            m.stopChan = make(chan struct{})
            m.mu.Unlock()

            if oldChan != nil {
                oldChan.Close()
            }
            if oldConn != nil {
                oldConn.Close()
            }

            go m.listenForResults()
            log.Println("RabbitMQ reconnected successfully")
        }
        time.Sleep(5 * time.Second)
    }
}

func NewManager(mongoURI, rabbitURI, tasksQueue, resultsQueue string, workersCount int, timeout time.Duration) (*Manager, error) {
    var mongoClient *mongo.Client
    var err error

    for i := 0; i < 10; i++ {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

        clientOpts := options.Client().
            ApplyURI(mongoURI).
            SetWriteConcern(writeconcern.Majority()).
            SetReadPreference(readpref.Primary())

        mongoClient, err = mongo.Connect(ctx, clientOpts)
        cancel()

        if err == nil {
            ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
            err = mongoClient.Ping(ctx, readpref.Primary())
            cancel()
            if err == nil {
                log.Println("Successfully connected to MongoDB primary")
                break
            }
        }

        log.Printf("Failed to connect to MongoDB (attempt %d/10): %v, retrying in 3 seconds...", i+1, err)
        time.Sleep(3 * time.Second)

        if i == 9 {
            return nil, fmt.Errorf("failed to connect to MongoDB after 10 attempts: %w", err)
        }
    }

    db := mongoClient.Database("crackhash")
    requestsColl := db.Collection("requests")
    outboxColl := db.Collection("outbox")

    conn, err := amqp.Dial(rabbitURI)
    if err != nil {
        return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
    }

    ch, err := conn.Channel()
    if err != nil {
        return nil, fmt.Errorf("failed to open RabbitMQ channel: %w", err)
    }

    _, err = ch.QueueDeclare(tasksQueue, true, false, false, false, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to declare tasks queue: %w", err)
    }
    _, err = ch.QueueDeclare(resultsQueue, true, false, false, false, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to declare results queue: %w", err)
    }

    m := &Manager{
        mongoClient:      mongoClient,
        requestsColl:     requestsColl,
        outboxColl:       outboxColl,
        rabbitConn:       conn,
        rabbitChan:       ch,
        tasksQueueName:   tasksQueue,
        resultsQueueName: resultsQueue,
        workersCount:     workersCount,
        timeout:          timeout,
        stopChan:         make(chan struct{}),
    }

    go m.monitorRabbitMQ(rabbitURI)
    go m.listenForResults()
    go m.processOutbox()

    log.Println("Manager initialized with MongoDB and RabbitMQ")
    return m, nil
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
    now := time.Now()

    doc := models.RequestDocument{
        ID:            requestID,
        Hash:          req.Hash,
        MaxLength:     req.MaxLength,
        Status:        StatusInProgress,
        CreatedAt:     now,
        UpdatedAt:     now,
        WordsFound:    []string{},
        TasksSent:     0,
        TasksExpected: m.workersCount,
        TasksReceived: 0,
    }

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    _, err := m.requestsColl.InsertOne(ctx, doc)
    if err != nil {
        log.Printf("Failed to save request to MongoDB: %v", err)
        http.Error(w, "Internal Server Error", http.StatusInternalServerError)
        return
    }

    log.Printf("Request %s saved to MongoDB", requestID)

    alphabet := "abcdefghijklmnopqrstuvwxyz0123456789"
    tasks := make([]models.WorkerTaskRequest, m.workersCount)
    for i := 0; i < m.workersCount; i++ {
        tasks[i] = models.WorkerTaskRequest{
            RequestID:  requestID,
            Hash:       req.Hash,
            MaxLength:  req.MaxLength,
            Alphabet:   alphabet,
            PartNumber: i + 1,
            PartCount:  m.workersCount,
        }
    }

    if err := m.publishTasks(requestID, tasks); err != nil {
        log.Printf("Failed to publish tasks to RabbitMQ, saving to outbox: %v", err)
        m.saveToOutbox(requestID, tasks)
    }

    resp := models.CrackHashResponse{RequestID: requestID}
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(resp)
}

func (m *Manager) publishTasks(requestID string, tasks []models.WorkerTaskRequest) error {
    for _, task := range tasks {
        body, err := json.Marshal(task)
        if err != nil {
            return err
        }

        err = m.rabbitChan.Publish(
            "",
            m.tasksQueueName,
            true,
            false,
            amqp.Publishing{
                ContentType:  "application/json",
                Body:         body,
                DeliveryMode: amqp.Persistent,
            })
        if err != nil {
            return err
        }
    }

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    filter := bson.M{"_id": requestID}
    update := bson.M{"$set": bson.M{"tasks_sent": len(tasks)}}
    _, err := m.requestsColl.UpdateOne(ctx, filter, update)
    if err != nil {
        log.Printf("Failed to update tasks_sent for request %s: %v", requestID, err)
    }

    return nil
}

func (m *Manager) saveToOutbox(requestID string, tasks []models.WorkerTaskRequest) {
    doc := models.OutboxDocument{
        ID:        uuid.New().String(),
        RequestID: requestID,
        Tasks:     tasks,
        CreatedAt: time.Now(),
    }

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    _, err := m.outboxColl.InsertOne(ctx, doc)
    if err != nil {
        log.Printf("Failed to save to outbox: %v", err)
    } else {
        log.Printf("Tasks for request %s saved to outbox", requestID)
    }
}

func (m *Manager) processOutbox() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        if m.rabbitChan == nil {
            continue
        }

        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        cursor, err := m.outboxColl.Find(ctx, bson.M{})
        cancel()

        if err != nil {
            log.Printf("Failed to query outbox: %v", err)
            continue
        }

        var docs []models.OutboxDocument
        if err := cursor.All(context.Background(), &docs); err != nil {
            log.Printf("Failed to decode outbox documents: %v", err)
            cursor.Close(context.Background())
            continue
        }
        cursor.Close(context.Background())

        for _, doc := range docs {
            if err := m.publishTasks(doc.RequestID, doc.Tasks); err == nil {
                ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
                _, err := m.outboxColl.DeleteOne(ctx, bson.M{"_id": doc.ID})
                cancel()
                if err != nil {
                    log.Printf("Failed to delete outbox document %s: %v", doc.ID, err)
                } else {
                    log.Printf("Outbox tasks for request %s sent successfully", doc.RequestID)
                }
            }
        }
    }
}

func (m *Manager) listenForResults() {
    for {
        select {
        case <-m.stopChan:
            log.Println("Stopping results listener")
            return
        default:
        }

        if m.rabbitChan == nil {
            time.Sleep(1 * time.Second)
            continue
        }

        msgs, err := m.rabbitChan.Consume(
            m.resultsQueueName,
            "manager",
            false,
            false,
            false,
            false,
            nil,
        )
        if err != nil {
            log.Printf("Failed to consume results queue: %v, retrying...", err)
            time.Sleep(5 * time.Second)
            continue
        }

        log.Println("Results listener started")
        for d := range msgs {
            select {
            case <-m.stopChan:
                log.Println("Stopping results listener during message processing")
                return
            default:
            }

            var result models.WorkerResponse
            if err := json.Unmarshal(d.Body, &result); err != nil {
                log.Printf("Failed to unmarshal result: %v", err)
                d.Nack(false, false)
                continue
            }

            m.processResult(result)
            d.Ack(false)
        }
        log.Println("Results channel closed, restarting listener...")
    }
}

func (m *Manager) processResult(result models.WorkerResponse) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    var doc models.RequestDocument
    err := m.requestsColl.FindOne(ctx, bson.M{"_id": result.RequestID}).Decode(&doc)
    if err != nil {
        log.Printf("Request %s not found in DB: %v", result.RequestID, err)
        return
    }

    if doc.Status != StatusInProgress {
        log.Printf("Request %s already completed (status=%s), ignoring result", result.RequestID, doc.Status)
        return
    }

    newWords := append(doc.WordsFound, result.Words...)
    newReceived := doc.TasksReceived + 1
    newStatus := doc.Status

    if newReceived >= doc.TasksExpected {
        newStatus = StatusReady
        log.Printf("Request %s completed with %d words", result.RequestID, len(newWords))
    }

    update := bson.M{
        "$set": bson.M{
            "words_found":    newWords,
            "tasks_received": newReceived,
            "status":         newStatus,
            "updated_at":     time.Now(),
        },
    }
    _, err = m.requestsColl.UpdateOne(ctx, bson.M{"_id": result.RequestID}, update)
    if err != nil {
        log.Printf("Failed to update request %s: %v", result.RequestID, err)
    }
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

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    var doc models.RequestDocument
    err := m.requestsColl.FindOne(ctx, bson.M{"_id": requestID}).Decode(&doc)
    if err != nil {
        http.Error(w, "Request not found", http.StatusNotFound)
        return
    }

    resp := models.StatusResponse{
        Status: doc.Status,
        Data:   doc.WordsFound,
    }
    if resp.Status != StatusReady {
        resp.Data = nil
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func (m *Manager) Close() {
    select {
    case <-m.stopChan:
    default:
        close(m.stopChan)
    }

    if m.rabbitChan != nil {
        m.rabbitChan.Close()
    }
    if m.rabbitConn != nil {
        m.rabbitConn.Close()
    }
    if m.mongoClient != nil {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        m.mongoClient.Disconnect(ctx)
    }
}