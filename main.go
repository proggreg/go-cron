package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"cloud.google.com/go/scheduler/apiv1"
	"github.com/gorilla/mux"
	schedulerpb "google.golang.org/genproto/googleapis/cloud/scheduler/v1"
)

// Job represents a scheduled job
type Job struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Schedule            string     `json:"schedule"`
	Command             string     `json:"command"`
	WebhookURL          string     `json:"webhook_url,omitempty"`
	WebhookPayload      string     `json:"webhook_payload,omitempty"`
	LastRun             *time.Time `json:"last_run,omitempty"`
	NextRun             *time.Time `json:"next_run,omitempty"`
	Status              string     `json:"status"`
	LastWebhookResponse string     `json:"last_webhook_response,omitempty"`
	CloudSchedulerJobName string   `json:"cloud_scheduler_job_name,omitempty"`
}

var (
	jobs            = make(map[string]*Job)
	jobsMutex       sync.RWMutex
	schedulerClient *scheduler.CloudSchedulerClient
	jobsFile        = "data/jobs.json"
	gcpProjectID    = os.Getenv("GCP_PROJECT_ID")
	gcpLocationID   = os.Getenv("GCP_LOCATION_ID")
	gcpServiceURL   = os.Getenv("GCP_SERVICE_URL")
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
	return nil
}

func main() {
	ctx := context.Background()
	var err error
	schedulerClient, err = scheduler.NewCloudSchedulerClient(ctx)
	if err != nil {
		log.Fatalf("Error creating Cloud Scheduler client: %v", err)
	}
	defer schedulerClient.Close()

	if err := loadJobs(); err != nil {
		log.Fatalf("Error loading jobs: %v", err)
	}

	// Save jobs immediately after loading and re-scheduling to persist new CronEntryIDs
	if err := saveJobs(); err != nil {
		log.Printf("Error saving jobs after load: %v", err)
	}

	r := mux.NewRouter()

	r.HandleFunc("/jobs/{id}/run", executeJobHandler).Methods("POST")
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

	// Create a job in Google Cloud Scheduler
	parent := fmt.Sprintf("projects/%s/locations/%s", gcpProjectID, gcpLocationID)
	cloudSchedulerJob := &schedulerpb.Job{
		Name:        fmt.Sprintf("%s/jobs/%s", parent, job.ID),
		Description: job.Name,
		Schedule:    job.Schedule,
		Target: &schedulerpb.Job_HttpTarget{
			HttpTarget: &schedulerpb.HttpTarget{
				Uri:        fmt.Sprintf("%s/jobs/%s/run", gcpServiceURL, job.ID),
				HttpMethod: schedulerpb.HttpMethod_POST,
			},
		},
	}

	ctx := context.Background()
	createdJob, err := schedulerClient.CreateJob(ctx, &schedulerpb.CreateJobRequest{
		Parent: parent,
		Job:    cloudSchedulerJob,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Error creating Cloud Scheduler job: %v", err), http.StatusInternalServerError)
		return
	}

	job.CloudSchedulerJobName = createdJob.Name

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

	// Update the job in Google Cloud Scheduler
	cloudSchedulerJob := &schedulerpb.Job{
		Name:        job.CloudSchedulerJobName,
		Description: update.Name,
		Schedule:    update.Schedule,
		Target: &schedulerpb.Job_HttpTarget{
			HttpTarget: &schedulerpb.HttpTarget{
				Uri:        fmt.Sprintf("%s/jobs/%s/run", gcpServiceURL, job.ID),
				HttpMethod: schedulerpb.HttpMethod_POST,
			},
		},
	}

	ctx := context.Background()
	updatedJob, err := schedulerClient.UpdateJob(ctx, &schedulerpb.UpdateJobRequest{
		Job: cloudSchedulerJob,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Error updating Cloud Scheduler job: %v", err), http.StatusInternalServerError)
		return
	}

	job.Name = update.Name
	job.Schedule = update.Schedule
	job.Command = update.Command
	job.WebhookURL = update.WebhookURL
	job.WebhookPayload = update.WebhookPayload
	job.CloudSchedulerJobName = updatedJob.Name

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
		// Delete the job from Google Cloud Scheduler
		ctx := context.Background()
		err := schedulerClient.DeleteJob(ctx, &schedulerpb.DeleteJobRequest{
			Name: job.CloudSchedulerJobName,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Error deleting Cloud Scheduler job: %v", err), http.StatusInternalServerError)
			jobsMutex.Unlock()
			return
		}
		delete(jobs, id)
	}
	jobsMutex.Unlock()
	if err := saveJobs(); err != nil {
		log.Printf("Error saving jobs: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func executeJobHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	jobsMutex.RLock()
	job, ok := jobs[id]
	jobsMutex.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

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

	w.WriteHeader(http.StatusOK)
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
