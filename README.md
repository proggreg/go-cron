# Go Cron Backend

A simple Go backend to manage long-running tasks (jobs) on a cron schedule, with a REST API to manage jobs.

## TODO
- [ ] Fix Swagger 
- [ ] Add persistent storage for jobs (e.g., BoltDB, SQLite)
- [ ] Implement authentication and authorization
- [ ] Execute real shell commands for jobs (with security considerations)
- [ ] Add job logs/history endpoint
- [ ] Add job pause/resume functionality
- [ ] Add job retry/backoff support
- [ ] Add unit and integration tests
- [ ] Add Dockerfile and deployment instructions
- [ ] Improve error handling and API responses
- [ ] Add support for job parameters/environment variables
- [ ] Add support for recurring jobs with limited runs
- [ ] Add metrics and health endpoints
- [ ] Improve frontend UI/UX


## Features
- Schedule jobs using cron expressions
- Create, list, update, delete, and trigger jobs via HTTP API
- In-memory job storage (no persistence)

## Requirements
- Go 1.21 or later

## Setup

1. Install dependencies:
   ```sh
   go mod tidy
   ```

2. Run the server:
   ```sh
   go run main.go
   ```

The server will start on `http://localhost:8080`.

## API Endpoints

- `GET    /jobs`           — List all jobs
- `POST   /jobs`           — Create a new job
- `GET    /jobs/{id}`      — Get a job by ID
- `PUT    /jobs/{id}`      — Update a job
- `DELETE /jobs/{id}`      — Delete a job
- `POST   /jobs/{id}/run`  — Trigger a job immediately

### Example Job JSON
```
{
  "name": "Example Job",
  "schedule": "@every 1m",
  "command": "echo hello",
  "webhook_url": "https://your-webhook-url.com",
  "webhook_payload": "{\"message\": \"Job finished!\"}"
}
```

- `schedule` uses [robfig/cron](https://pkg.go.dev/github.com/robfig/cron/v3#hdr-CRON_Expression_Format) syntax.
- `command` is a placeholder string (actual command execution is not implemented; jobs just simulate a long-running task).
- `webhook_url` (optional) is the URL to send a POST request to when the job finishes.
- `webhook_payload` (optional) is the JSON payload to send with the webhook.


## Notes
- This is a minimal example for demonstration. For production, add persistence, authentication, and real command execution.

# Accessing the Frontend and Swagger UI

- The htmx-based test frontend will be available at http://localhost:8080/ (root path).
- The Swagger UI will be available at http://localhost:8080/swagger-ui/ (after you enable the route in main.go).
- The OpenAPI YAML is at http://localhost:8080/swagger.yaml

> **Note:** You must create a `frontend` directory with an `index.html` file for the htmx frontend to work. 