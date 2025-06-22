package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/robfig/cron/v3"
)

// Job represents a scheduled job
type Job struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Schedule    string       `json:"schedule"`
	Command     string       `json:"command"`
	LastRun     *time.Time   `json:"last_run,omitempty"`
	NextRun     *time.Time   `json:"next_run,omitempty"`
	Status      string       `json:"status"`
	CronEntryID cron.EntryID `json:"-"`
}

var (
	jobs          = make(map[string]*Job)
	jobsMutex     sync.RWMutex
	cronScheduler = cron.New()
)

func main() {
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
	jobsMutex.Lock()
	job.Status = "scheduled"
	jobsMutex.Unlock()
}
