package main

import (
	"embed"
	"encoding/json"
	"errors"

	"io"
	"io/fs"
	"net"
	"net/http"
	"strconv"

	"github.com/tathagata/coderead/internal/core"
	"github.com/tathagata/coderead/internal/model"
)

//go:embed ui/*
var uiFiles embed.FS

type App struct {
	repository   *core.Repository
	explanations *core.ExplanationService
	cfg          Config
}

func NewApp(index *core.Index, cfg Config) *App {
	return &App{repository: core.NewRepository(index), explanations: core.NewExplanationService(cfg, model.Call), cfg: cfg}
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(uiFiles, "ui")
	mux.Handle("/", http.FileServer(http.FS(assets)))
	mux.HandleFunc("GET /api/tree", a.tree)
	mux.HandleFunc("POST /api/refresh", a.refresh)
	mux.HandleFunc("GET /api/file", a.file)
	mux.HandleFunc("POST /api/explain", a.explain)
	mux.HandleFunc("POST /api/explain/preview", a.preview)
	mux.HandleFunc("GET /api/overview", a.overview)
	mux.HandleFunc("GET /api/node", a.node)
	mux.HandleFunc("GET /api/evidence", a.evidence)
	mux.HandleFunc("GET /api/tour", a.tour)
	mux.HandleFunc("GET /api/review", a.review)
	mux.HandleFunc("GET /api/commands", func(w http.ResponseWriter, _ *http.Request) { jsonResponse(w, 200, core.Commands()) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, port, err := net.SplitHostPort(r.Host)
		portNumber, portErr := strconv.Atoi(port)
		if err != nil || portErr != nil || portNumber < 1 || portNumber > 65535 || (host != "127.0.0.1" && host != "localhost") {
			http.Error(w, "local access only", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && r.Header.Get("Origin") != "http://"+r.Host {
			http.Error(w, "same-origin requests only", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		mux.ServeHTTP(w, r)
	})
}

func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func errorResponse(w http.ResponseWriter, status int, err error) {
	jsonResponse(w, status, map[string]string{"error": err.Error()})
}

func (a *App) tree(w http.ResponseWriter, r *http.Request) {
	idx := a.repository.Snapshot()
	if err := idx.CheckPolicy(); err != nil {
		serviceError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"files": idx.Files, "provider": a.cfg.Provider, "model": a.cfg.Model, "used_input_tokens": a.explanations.Used(), "session_input_budget": a.cfg.SessionInputBudget})
}

func (a *App) refresh(w http.ResponseWriter, r *http.Request) {
	if err := a.repository.Refresh(); err != nil {
		serviceError(w, err)
		return
	}
	a.tree(w, r)
}

func (a *App) file(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	idx := a.repository.Snapshot()
	source, entry, err := idx.Read(path)
	if err != nil {
		var classified *core.Error
		if errors.As(err, &classified) {
			serviceError(w, err)
			return
		}
		errorResponse(w, 404, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"path": path, "source": source, "kind": entry.Kind, "units": idx.CurrentUnits(path, entry, source)})
}

type ExplainRequest struct {
	Path     string `json:"path"`
	UnitID   string `json:"unit_id"`
	Digest   string `json:"digest"`
	Approved bool   `json:"approved"`
	Enrich   bool   `json:"enrich"`
}

func decodeExplanation(w http.ResponseWriter, r *http.Request) (ExplainRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input ExplainRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return input, errors.New("expected one JSON object")
	}
	return input, nil
}

func (a *App) preview(w http.ResponseWriter, r *http.Request) {
	input, err := decodeExplanation(w, r)
	if err != nil {
		errorResponse(w, 400, err)
		return
	}
	idx, g := a.repository.View()
	prepared, err := a.explanations.Prepare(r.Context(), idx, g, input.Path, input.UnitID, input.Enrich)
	if err != nil {
		serviceError(w, err)
		return
	}
	jsonResponse(w, 200, prepared)
}

func (a *App) explain(w http.ResponseWriter, r *http.Request) {
	input, err := decodeExplanation(w, r)
	if err != nil {
		errorResponse(w, 400, err)
		return
	}
	if !input.Approved {
		errorResponse(w, 400, errors.New("explicit approval required before sending code to the model"))
		return
	}
	idx, g := a.repository.View()
	prepared, err := a.explanations.Prepare(r.Context(), idx, g, input.Path, input.UnitID, input.Enrich)
	if err != nil {
		serviceError(w, err)
		return
	}
	if input.Digest == "" || input.Digest != prepared.Digest {
		errorResponse(w, 409, errors.New("approved context changed or preview missing; preview again before sending"))
		return
	}
	result, err := a.explanations.Send(r.Context(), prepared)
	if err != nil {
		serviceError(w, err)
		return
	}
	jsonResponse(w, 200, result)
}

func (a *App) overview(w http.ResponseWriter, _ *http.Request) {
	result, err := a.repository.Overview()
	if err != nil {
		serviceError(w, err)
		return
	}
	jsonResponse(w, 200, result)
}
func (a *App) node(w http.ResponseWriter, r *http.Request) {
	result, err := a.repository.Orient(r.URL.Query().Get("id"))
	if err != nil {
		serviceError(w, err)
		return
	}
	jsonResponse(w, 200, result)
}
func (a *App) evidence(w http.ResponseWriter, r *http.Request) {
	result, err := a.repository.Evidence(r.Context(), r.URL.Query().Get("id"))
	if err != nil {
		serviceError(w, err)
		return
	}
	jsonResponse(w, 200, result)
}
func (a *App) tour(w http.ResponseWriter, r *http.Request) {
	result, err := a.repository.Tour(r.Context(), r.URL.Query().Get("kind"))
	if err != nil {
		serviceError(w, err)
		return
	}
	jsonResponse(w, 200, result)
}

func serviceError(w http.ResponseWriter, err error) {
	status := 500
	var failure *core.Error
	if errors.As(err, &failure) {
		switch failure.Kind {
		case core.Invalid:
			status = 400
		case core.NotFound:
			status = 404
		case core.TooLarge:
			status = 413
		case core.BudgetExceeded:
			status = 429
		case core.ModelFailed:
			status = 502
		case core.Stale:
			status = 409
		}
	}
	errorResponse(w, status, err)
}

func (a *App) review(w http.ResponseWriter, r *http.Request) {
	result, err := a.repository.Review(r.Context(), r.URL.Query().Get("base"))
	if err != nil {
		serviceError(w, err)
		return
	}
	jsonResponse(w, 200, result)
}
