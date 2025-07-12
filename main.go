package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/robfig/cron/v3"
)

// Job represents a scheduled job
type Job struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Schedule       string       `json:"schedule"`
	Command        string       `json:"command"`
	WebhookURL     string       `json:"webhook_url,omitempty"`
	WebhookPayload string       `json:"webhook_payload,omitempty"`
	LastRun        *time.Time   `json:"last_run,omitempty"`
	NextRun        *time.Time   `json:"next_run,omitempty"`
	Status         string       `json:"status"`
	LastWebhookResponse string    `json:"last_webhook_response,omitempty"`
	CronEntryID    cron.EntryID `json:"-"`
}

var (
	jobs          = make(map[string]*Job)
	jobsMutex     sync.RWMutex
	cronScheduler = cron.New()
	jobsFile      = "data/jobs.json"
)

func saveJobs() error {
	jobsMutex.RLock()
	defer jobsMutex.RUnlock()

	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(jobsFile, data, 0644)
}

func loadJobs() error {
	jobsMutex.Lock()
	defer jobsMutex.Unlock()

	data, err := os.ReadFile(jobsFile)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Jobs file %s not found, starting with empty jobs.", jobsFile)
			return nil // File doesn't exist, start with empty jobs
		}
		return err
	}

	// If the file is empty, start with an empty map of jobs.
	if len(data) == 0 {
		jobs = make(map[string]*Job)
		return nil
	}

	var loadedJobs map[string]*Job
	if err := json.Unmarshal(data, &loadedJobs); err != nil {
		return err
	}

	jobs = loadedJobs
	// Re-add jobs to cron scheduler
	for _, job := range jobs {
		entryID, err := cronScheduler.AddFunc(job.Schedule, func(j *Job) func() {
			return func() { executeJob(j) }
		}(job))
		if err != nil {
			log.Printf("Error re-scheduling job %s: %v", job.ID, err)
			continue
		}
		job.CronEntryID = entryID
	}
	return nil
}

func main() {
	if err := loadJobs(); err != nil {
		log.Fatalf("Error loading jobs: %v", err)
	}

	// Save jobs immediately after loading and re-scheduling to persist new CronEntryIDs
	if err := saveJobs(); err != nil {
		log.Printf("Error saving jobs after load: %v", err)
	}

	cronScheduler.Start()
	defer cronScheduler.Stop()

	r := mux.NewRouter()

	r.HandleFunc("/jobs/{id}/run", runJobNow).Methods("POST")
	r.HandleFunc("/jobs/{id}", getJob).Methods("GET")
	r.HandleFunc("/jobs/{id}", updateJob).Methods("PUT")
	r.HandleFunc("/jobs/{id}", deleteJob).Methods("DELETE")
	r.HandleFunc("/jobs", listJobs).Methods("GET")
	r.HandleFunc("/jobs", createJob).Methods("POST")

	r.PathPrefix("/swagger-ui/").Handler(http.StripPrefix("/swagger-ui/", http.FileServer(http.Dir("swagger-ui"))))
	r.Handle("/swagger.yaml", http.FileServer(http.Dir(".")))

	r.HandleFunc("/webhook-test", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received webhook test request: %s", r.Method)
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		log.Printf("Webhook test request body: %s", buf.String())
		w.WriteHeader(http.StatusOK)
	}).Methods("POST")

	r.PathPrefix("/").Handler(http.FileServer(http.Dir("frontend")))

	log.Println("Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}

func listJobs(w http.ResponseWriter, r *http.Request) {
	jobsMutex.RLock()
	defer jobsMutex.RUnlock()
	var jobList []*Job
	for _, job := range jobs {
		jobList = append(jobList, job)
	}
	json.NewEncoder(w).Encode(jobList)
}

func createJob(w http.ResponseWriter, r *http.Request) {
	var job Job
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if job.ID == "" {
		job.ID = time.Now().Format("20060102150405.000000")
	}
	job.Status = "scheduled"

	entryID, err := cronScheduler.AddFunc(job.Schedule, func() {
		executeJob(&job)
	})
	if err != nil {
		http.Error(w, "Invalid cron schedule", http.StatusBadRequest)
		return
	}
	job.CronEntryID = entryID

	jobsMutex.Lock()
	jobs[job.ID] = &job
	jobsMutex.Unlock()

	if err := saveJobs(); err != nil {
		log.Printf("Error saving jobs: %v", err)
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(job)
}

func getJob(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	jobsMutex.RLock()
	job, ok := jobs[id]
	jobsMutex.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	json.NewEncoder(w).Encode(job)
}

func updateJob(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	jobsMutex.Lock()
	job, ok := jobs[id]
	jobsMutex.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	var update Job
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Remove old cron job
	cronScheduler.Remove(job.CronEntryID)
	job.Name = update.Name
	job.Schedule = update.Schedule
	job.Command = update.Command
	job.WebhookURL = update.WebhookURL
	job.WebhookPayload = update.WebhookPayload
	entryID, err := cronScheduler.AddFunc(job.Schedule, func() {
		executeJob(job)
	})
	if err != nil {
		http.Error(w, "Invalid cron schedule", http.StatusBadRequest)
		return
	}
	job.CronEntryID = entryID
	jobsMutex.Lock()
	jobs[id] = job
	jobsMutex.Unlock()
	if err := saveJobs(); err != nil {
		log.Printf("Error saving jobs: %v", err)
	}
	json.NewEncoder(w).Encode(job)
}

func deleteJob(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	jobsMutex.Lock()
	job, ok := jobs[id]
	if ok {
		cronScheduler.Remove(job.CronEntryID)
		delete(jobs, id)
	}
	jobsMutex.Unlock()
	if err := saveJobs(); err != nil {
		log.Printf("Error saving jobs: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func runJobNow(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	jobsMutex.RLock()
	job, ok := jobs[id]
	jobsMutex.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	go executeJob(job)
	w.WriteHeader(http.StatusAccepted)
}

func executeJob(job *Job) {
	jobsMutex.Lock()
	now := time.Now()
	job.LastRun = &now
	job.Status = "running"
	jobsMutex.Unlock()
	// Simulate long-running task
	time.Sleep(5 * time.Second)

	if job.WebhookURL != "" {
		go sendWebhook(job)
	}

	jobsMutex.Lock()
	job.Status = "scheduled"
	jobsMutex.Unlock()
}

func sendWebhook(job *Job) {
	payload := []byte(job.WebhookPayload)
	req, err := http.NewRequest("POST", job.WebhookURL, bytes.NewBuffer(payload))
	if err != nil {
		log.Printf("Error creating webhook request for job %s: %v", job.ID, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error sending webhook for job %s: %v", job.ID, err)
		return
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading webhook response body for job %s: %v", job.ID, err)
			return
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					log.Printf("Webhook sent successfully for job %s. Response: %s", job.ID, string(responseBody))
		jobsMutex.Lock()
		job.LastWebhookResponse = string(responseBody)
		jobsMutex.Unlock()
	} else {
		log.Printf("Webhook for job %s failed with status code: %d. Response: %s", job.ID, resp.StatusCode, string(responseBody))
		jobsMutex.Lock()
		job.LastWebhookResponse = string(responseBody)
		jobsMutex.Unlock()
	}
}
