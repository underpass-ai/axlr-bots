package unixapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// maxBodyBytes bounds a request body; a bot definition or an objective
// is far smaller.
const maxBodyBytes = 1 << 20

// Server serves the Service over HTTP on a Unix socket.
type Server struct {
	service Service
}

// NewServer serves the service.
func NewServer(service Service) *Server {
	return &Server{service: service}
}

// Handler is the API's routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/bots", s.registerBot)
	mux.HandleFunc("GET /v1/bots", s.listBots)
	mux.HandleFunc("GET /v1/bots/{id}", s.getBot)
	mux.HandleFunc("POST /v1/bots/{id}/enable", s.enableBot)
	mux.HandleFunc("POST /v1/tasks", s.submitTask)
	mux.HandleFunc("POST /v1/alerts", s.alert)
	mux.HandleFunc("GET /v1/tasks/{id}", s.getTask)
	mux.HandleFunc("POST /v1/tasks/{id}/run", s.runTask)
	mux.HandleFunc("POST /v1/tasks/{id}/decide", s.decideTask)
	mux.HandleFunc("GET /v1/tasks/{id}/report", s.getReport)
	mux.HandleFunc("GET /v1/executions/{id}", s.getExecution)
	return mux
}

// Serve listens on the socket until ctx ends. A socket file left by a
// dead daemon is removed first; the file is 0660 so the service's user
// and group reach it and nobody else.
func (s *Server) Serve(ctx context.Context, socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return fmt.Errorf("unixapi: %w", err)
	}
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("unixapi: stale socket: %w", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("unixapi: %w", err)
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		_ = listener.Close()
		return fmt.Errorf("unixapi: %w", err)
	}
	server := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		<-done
		_ = os.Remove(socketPath)
		return nil
	case err := <-done:
		_ = os.Remove(socketPath)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("unixapi: %w", err)
	}
}

func (s *Server) registerBot(w http.ResponseWriter, r *http.Request) {
	var definition botDefinition
	if !readJSON(w, r, &definition) {
		return
	}
	value, err := s.service.RegisterBot(r.Context(), definition.BotDefinition)
	respond(w, http.StatusCreated, value, err)
}

func (s *Server) listBots(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.ListBots(r.Context())
	respond(w, http.StatusOK, value, err)
}

func (s *Server) getBot(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetBot(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) enableBot(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.EnableBot(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) submitTask(w http.ResponseWriter, r *http.Request) {
	var request SubmitRequest
	if !readJSON(w, r, &request) {
		return
	}
	response, err := s.service.SubmitTask(r.Context(), request)
	respond(w, createdStatus(response.Created), response, err)
}

func (s *Server) alert(w http.ResponseWriter, r *http.Request) {
	var request AlertRequest
	if !readJSON(w, r, &request) {
		return
	}
	response, err := s.service.Alert(r.Context(), request)
	respond(w, createdStatus(response.Created), response, err)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetTask(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) runTask(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.RunTask(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) decideTask(w http.ResponseWriter, r *http.Request) {
	var request DecideRequest
	if !readJSON(w, r, &request) {
		return
	}
	value, err := s.service.DecideTask(r.Context(), r.PathValue("id"), request.Decision)
	respond(w, http.StatusOK, value, err)
}

func (s *Server) getReport(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetReport(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) getExecution(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetExecution(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func createdStatus(created bool) int {
	if created {
		return http.StatusCreated
	}
	return http.StatusOK
}

// readJSON decodes the body, refusing unknown fields; false means the
// error was already written.
func readJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Kind: KindValidation, Message: "request body: " + err.Error()})
		return false
	}
	return true
}

// respond writes the result with the status, or the error classified.
func respond(w http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		code, response := toWire(err)
		writeJSON(w, code, response)
		return
	}
	writeJSON(w, status, value)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
