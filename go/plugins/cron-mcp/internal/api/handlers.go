package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/db"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/scheduler"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/service"
	"gorm.io/gorm"
)

// Board groups jobs by status.
type Board struct {
	Groups []Group `json:"groups"`
}

// Group holds jobs for a single status.
type Group struct {
	Status string        `json:"status"`
	Jobs   []*db.CronJob `json:"jobs"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func httpStatus(err error) int {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return http.StatusNotFound
	}
	msg := err.Error()
	if strings.Contains(msg, "invalid status") {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func parseID(path, prefix string) (uint, string, bool) {
	trimmed := strings.TrimPrefix(path, prefix)
	parts := strings.SplitN(trimmed, "/", 2)
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	suffix := ""
	if len(parts) > 1 {
		suffix = "/" + parts[1]
	}
	return uint(id), suffix, true
}

// JobsHandler handles /api/jobs (GET list, POST create).
func JobsHandler(svc *service.CronService, sched *scheduler.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			filter := service.JobFilter{}
			if s := r.URL.Query().Get("status"); s != "" {
				js := db.JobStatus(s)
				filter.Status = &js
			}
			if l := r.URL.Query().Get("label"); l != "" {
				filter.Label = &l
			}
			jobs, err := svc.ListJobs(r.Context(), filter)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, jobs)

		case http.MethodPost:
			var body struct {
				Name        string   `json:"name"`
				Description string   `json:"description"`
				Schedule    string   `json:"schedule"`
				Command     string   `json:"command"`
				Labels      []string `json:"labels"`
				Timeout     int      `json:"timeout"`
				MaxRetries  int      `json:"max_retries"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
			req := service.CreateJobRequest{
				Name:        body.Name,
				Description: body.Description,
				Schedule:    body.Schedule,
				Command:     body.Command,
				Labels:      body.Labels,
				Timeout:     body.Timeout,
				MaxRetries:  body.MaxRetries,
			}
			job, err := svc.CreateJob(r.Context(), req)
			if err != nil {
				writeError(w, httpStatus(err), err.Error())
				return
			}
			if sched != nil {
				sched.AddJob(job)
			}
			writeJSON(w, http.StatusCreated, job)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// JobHandler handles /api/jobs/{id}, /api/jobs/{id}/run, /api/jobs/{id}/toggle, /api/jobs/{id}/executions.
func JobHandler(svc *service.CronService, sched *scheduler.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, suffix, ok := parseID(r.URL.Path, "/api/jobs/")
		if !ok {
			http.NotFound(w, r)
			return
		}

		switch suffix {
		case "/run":
			handleRunJob(w, r, svc, sched, id)
		case "/toggle":
			handleToggleJob(w, r, svc, sched, id)
		case "/executions":
			handleExecutions(w, r, svc, id)
		case "":
			handleJob(w, r, svc, sched, id)
		default:
			http.NotFound(w, r)
		}
	}
}

// ExecutionHandler handles /api/executions/{id}.
func ExecutionHandler(svc *service.CronService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		trimmed := strings.TrimPrefix(r.URL.Path, "/api/executions/")
		eid, err := strconv.ParseUint(trimmed, 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		exec, err := svc.GetExecution(r.Context(), uint(eid))
		if err != nil {
			writeError(w, httpStatus(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, exec)
	}
}

func handleJob(w http.ResponseWriter, r *http.Request, svc *service.CronService, sched *scheduler.Scheduler, id uint) {
	switch r.Method {
	case http.MethodGet:
		job, err := svc.GetJob(r.Context(), id)
		if err != nil {
			writeError(w, httpStatus(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, job)

	case http.MethodPut:
		var body struct {
			Name        *string   `json:"name"`
			Description *string   `json:"description"`
			Schedule    *string   `json:"schedule"`
			Command     *string   `json:"command"`
			Status      *string   `json:"status"`
			Labels      *[]string `json:"labels"`
			Timeout     *int      `json:"timeout"`
			MaxRetries  *int      `json:"max_retries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		req := service.UpdateJobRequest{
			Name:        body.Name,
			Description: body.Description,
			Schedule:    body.Schedule,
			Command:     body.Command,
			Labels:      body.Labels,
			Timeout:     body.Timeout,
			MaxRetries:  body.MaxRetries,
		}
		if body.Status != nil {
			s := db.JobStatus(*body.Status)
			req.Status = &s
		}
		job, err := svc.UpdateJob(r.Context(), id, req)
		if err != nil {
			writeError(w, httpStatus(err), err.Error())
			return
		}
		if sched != nil {
			sched.AddJob(job)
		}
		writeJSON(w, http.StatusOK, job)

	case http.MethodDelete:
		if sched != nil {
			sched.RemoveJob(id)
		}
		if err := svc.DeleteJob(r.Context(), id); err != nil {
			writeError(w, httpStatus(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleRunJob(w http.ResponseWriter, r *http.Request, svc *service.CronService, sched *scheduler.Scheduler, id uint) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	job, err := svc.GetJob(r.Context(), id)
	if err != nil {
		writeError(w, httpStatus(err), err.Error())
		return
	}
	if sched != nil {
		sched.RunNow(job.ID, job.Command, job.Timeout)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"triggered": true, "id": id})
}

func handleToggleJob(w http.ResponseWriter, r *http.Request, svc *service.CronService, sched *scheduler.Scheduler, id uint) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	job, err := svc.ToggleJob(r.Context(), id)
	if err != nil {
		writeError(w, httpStatus(err), err.Error())
		return
	}
	if sched != nil {
		sched.AddJob(job)
	}
	writeJSON(w, http.StatusOK, job)
}

func handleExecutions(w http.ResponseWriter, r *http.Request, svc *service.CronService, jobID uint) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	execs, err := svc.ListExecutions(r.Context(), jobID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, execs)
}

// BoardHandler handles GET /api/board.
func BoardHandler(svc *service.CronService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		jobs, err := svc.GetAllJobs(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		byStatus := make(map[db.JobStatus][]*db.CronJob)
		for _, j := range jobs {
			byStatus[j.Status] = append(byStatus[j.Status], j)
		}

		groups := make([]Group, 0, len(db.StatusList))
		for _, status := range db.StatusList {
			g := Group{
				Status: string(status),
				Jobs:   byStatus[status],
			}
			if g.Jobs == nil {
				g.Jobs = []*db.CronJob{}
			}
			groups = append(groups, g)
		}

		writeJSON(w, http.StatusOK, Board{Groups: groups})
	}
}
