package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"mime"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
	"github.com/dispatchlabs-ai/books/internal/banking"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"
	storesqlite "github.com/dispatchlabs-ai/books/internal/store/sqlite"
)

type Server struct {
	config      Config
	companies   map[string]*application.Service
	mu          sync.Mutex
	workerError bool
}

func New(ctx context.Context, booksConfig string, c Config) (*Server, error) {
	if e := c.Validate(); e != nil {
		return nil, e
	}
	s := &Server{config: c, companies: map[string]*application.Service{}}
	keys := map[string]bool{}
	for _, p := range c.Principals {
		for key := range p.Companies {
			keys[key] = true
		}
	}
	for key := range keys {
		app, e := application.Open(ctx, booksConfig, key, "books-import-worker", storesqlite.ReadWrite)
		if e != nil {
			_ = s.Close()
			return nil, e
		}
		s.companies[key] = app
	}
	return s, nil
}
func (s *Server) Close() error {
	var errs []error
	for _, app := range s.companies {
		if e := app.Close(); e != nil {
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}

// Serve owns worker lifetime independently of individual HTTP requests. Pending
// uploads remain queued in SQLite across disconnects and process restarts.
func (s *Server) Serve(ctx context.Context) error {
	listener, e := net.Listen("tcp", s.config.Listen)
	if e != nil {
		return apperr.Wrap(apperr.Unavailable, "SERVER_LISTEN_FAILED", "could not open the Books listener", e)
	}
	server := &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); s.work(workerCtx) }()
	shutdownDone := make(chan struct{})
	serveDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
		case <-serveDone:
			return
		}
		shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if server.Shutdown(shutdownCtx) != nil {
			_ = server.Close()
		}
	}()
	if s.config.TLSCertificate != "" {
		e = server.ServeTLS(listener, s.config.TLSCertificate, s.config.TLSKey)
	} else {
		e = server.Serve(listener)
	}
	close(serveDone)
	cancel()
	<-workerDone
	<-shutdownDone
	if e == http.ErrServerClosed {
		return nil
	}
	return apperr.Wrap(apperr.Unavailable, "SERVER_FAILED", "Books HTTP service stopped unexpectedly", e)
}
func (s *Server) work(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		s.ProcessPending(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Server) ProcessPending(ctx context.Context) {
	failed := false
	for _, app := range s.companies {
		if ctx.Err() != nil {
			return
		}
		ids, e := app.Pending(ctx)
		if e != nil {
			failed = true
			continue
		}
		for _, id := range ids {
			if _, e = app.Process(ctx, id); e != nil {
				failed = true
			}
		}
	}
	s.mu.Lock()
	s.workerError = failed
	s.mu.Unlock()
}
func (s *Server) principal(r *http.Request) (Principal, bool) {
	if len(r.Header.Values("Authorization")) != 1 {
		return Principal{}, false
	}
	auth := strings.Fields(r.Header.Get("Authorization"))
	if len(auth) != 2 || auth[0] != "Bearer" || len(auth[1]) < 32 || len(auth[1]) > 512 {
		return Principal{}, false
	}
	hash := sha256.Sum256([]byte(auth[1]))
	var found Principal
	ok := false
	for _, p := range s.config.Principals {
		expected, _ := hex.DecodeString(p.TokenSHA256)
		if subtle.ConstantTimeCompare(hash[:], expected) == 1 {
			found = p
			ok = true
		}
	}
	return found, ok
}
func granted(p Principal, company, permission string) bool {
	for _, g := range p.Companies[company] {
		if g == permission {
			return true
		}
	}
	return false
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	origin := r.Header.Get("Origin")
	if origin != "" {
		allowed := false
		for _, o := range s.config.AllowedOrigins {
			if o == origin {
				allowed = true
			}
		}
		if !allowed {
			writeFailure(w, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "browser origin is not allowed")
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Expose-Headers", "Location, ETag")
	}
	if r.Method == http.MethodOptions {
		if origin == "" {
			writeFailure(w, http.StatusForbidden, "ORIGIN_REQUIRED", "preflight requires an allowed origin")
			return
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, If-Match")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	p, ok := s.principal(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeFailure(w, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "valid bearer credentials are required")
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/capabilities" {
		writeData(w, http.StatusOK, map[string]any{"api": "books.api/v1", "formats": statementFormatNames(), "format_profiles": banking.Capabilities(), "parser": banking.StatementParserVersion, "supported_parsers": []string{banking.ParserVersion, banking.StatementParserVersion}, "sgml_versions": []string{"102", "103", "160"}, "xml": "OFX 2 bank/card subset", "currency": money.SupportedCurrencies(), "single_currency_per_entity": true, "currency_conversion": false, "max_upload_bytes": banking.MaxBytes, "durable_imports": true, "atomic_apply": true, "workflows": []string{"transactions", "corrections", "reconciliation", "period-close", "year-close", "accounts", "periods", "defaults"}, "local_administration": []string{"company-registry", "backup-restore", "quickbooks", "retained-lifecycle-evidence", "consolidation"}, "offline_posting": false, "change_feed": false, "posting_contra_types": []string{"REVENUE", "EXPENSE", "EQUITY"}})
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/companies" {
		keys := []string{}
		for k := range p.Companies {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		values := []application.Company{}
		for _, k := range keys {
			values = append(values, s.companies[k].Company())
		}
		writeData(w, http.StatusOK, values)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/health" {
		s.mu.Lock()
		failed := s.workerError
		s.mu.Unlock()
		if failed {
			writeFailure(w, http.StatusServiceUnavailable, "IMPORT_WORKER_UNAVAILABLE", "import worker encountered a storage failure")
			return
		}
		writeData(w, http.StatusOK, map[string]any{"status": "ready"})
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 || parts[0] != "v1" || parts[1] != "companies" {
		writeFailure(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "API route was not found")
		return
	}
	company := parts[2]
	app, exists := s.companies[company]
	if !exists || !granted(p, company, "read") {
		writeFailure(w, http.StatusNotFound, "COMPANY_NOT_FOUND", "company is not available to this principal")
		return
	}
	app = app.AsActor(p.ID)
	isMatches := len(parts) == 6 && parts[3] == "imports" && parts[5] == "matches"
	isImport := parts[3] == "imports" || parts[3] == "import-plans"
	if r.Method == http.MethodPost && isImport && !isMatches && !granted(p, company, "import") {
		writeFailure(w, http.StatusForbidden, "PERMISSION_DENIED", "import permission is required")
		return
	}
	if e := s.route(w, r, p, company, app, parts[3:]); e != nil {
		writeError(w, e)
	}
}
func (s *Server) route(w http.ResponseWriter, r *http.Request, p Principal, company string, app *application.Service, path []string) error {
	if handled, err := serveWorkflow(w, r, p, company, app, path); handled {
		return err
	}
	ctx := r.Context()
	resource := strings.Join(path, "/")
	if r.Method == http.MethodGet {
		switch resource {
		case "accounts":
			v, e := app.Accounts(ctx)
			if e != nil {
				return e
			}
			writeData(w, http.StatusOK, v)
			return nil
		case "statement-accounts":
			v, e := app.StatementAccounts(ctx)
			if e != nil {
				return e
			}
			writeData(w, http.StatusOK, v)
			return nil
		case "transactions":
			return serveTransactions(w, r, app)
		case "reports/general-ledger":
			return serveGeneralLedger(w, r, app)
		case "reports/trial-balance":
			v, e := app.TrialBalance(ctx, r.URL.Query().Get("as_of"))
			if e != nil {
				return e
			}
			writeData(w, http.StatusOK, v)
			return nil
		case "reports/balance-sheet":
			v, e := app.BalanceSheet(ctx, r.URL.Query().Get("as_of"))
			if e != nil {
				return e
			}
			writeData(w, http.StatusOK, v)
			return nil
		case "reports/profit-loss":
			v, e := app.ProfitLoss(ctx, r.URL.Query().Get("from"), r.URL.Query().Get("to"))
			if e != nil {
				return e
			}
			writeData(w, http.StatusOK, v)
			return nil
		}
		if len(path) == 2 && path[0] == "imports" {
			v, e := app.Job(ctx, path[1])
			if e != nil {
				return e
			}
			writeData(w, http.StatusOK, v)
			return nil
		}
		if len(path) == 3 && path[0] == "imports" && path[2] == "source" {
			data, e := app.Source(ctx, path[1])
			if e != nil {
				return e
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			job, e := app.Job(ctx, path[1])
			if e != nil {
				return e
			}
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": job.SourceName}))
			_, _ = w.Write(data)
			return nil
		}
		if len(path) == 2 && path[0] == "import-plans" {
			v, e := app.Plan(ctx, path[1])
			if e != nil {
				return e
			}
			w.Header().Set("ETag", `"`+v.Digest+`"`)
			writeData(w, http.StatusOK, v)
			return nil
		}
	}
	if r.Method == http.MethodPost {
		if resource == "imports" {
			data, name, options, e := readStatementUpload(w, r)
			if e != nil {
				return e
			}
			v, e := app.UploadWithOptions(ctx, r.Header.Get("Idempotency-Key"), name, data, options)
			if e != nil {
				return e
			}
			w.Header().Set("Location", "/v1/companies/"+company+"/imports/"+v.ID)
			writeData(w, http.StatusAccepted, v)
			return nil
		}
		if len(path) == 3 && path[0] == "imports" && path[2] == "matches" {
			var choices ledger.BankImportChoices
			if e := readJSON(w, r, &choices); e != nil {
				return e
			}
			result, e := app.Matches(ctx, path[1], choices)
			if e != nil {
				return e
			}
			writeData(w, http.StatusOK, result)
			return nil
		}
		if len(path) == 3 && path[0] == "imports" && path[2] == "previews" {
			var choices ledger.BankImportChoices
			if e := readJSON(w, r, &choices); e != nil {
				return e
			}
			if choices.Post && !granted(p, company, "post") {
				writeFailure(w, http.StatusForbidden, "PERMISSION_DENIED", "posting permission is required")
				return nil
			}
			v, e := app.Preview(ctx, path[1], r.Header.Get("Idempotency-Key"), choices)
			if e != nil {
				return e
			}
			w.Header().Set("ETag", `"`+v.Digest+`"`)
			writeData(w, http.StatusCreated, v)
			return nil
		}
		if len(path) == 3 && path[0] == "import-plans" && path[2] == "apply" {
			if r.ContentLength != 0 {
				return apperr.New(apperr.Input, "APPLY_BODY_NOT_ALLOWED", "apply has no request body; use If-Match with the preview digest")
			}
			plan, e := app.Plan(ctx, path[1])
			if e != nil {
				return e
			}
			if plan.Choices.Post && !granted(p, company, "post") {
				writeFailure(w, http.StatusForbidden, "PERMISSION_DENIED", "posting permission is required")
				return nil
			}
			expected := r.Header.Get("If-Match")
			if expected != `"`+plan.Digest+`"` {
				return apperr.New(apperr.Conflict, "IMPORT_PLAN_MISMATCH", "If-Match must contain the quoted preview digest")
			}
			v, e := app.Apply(ctx, path[1], plan.Digest)
			if e != nil {
				return e
			}
			writeData(w, http.StatusOK, v)
			return nil
		}
	}
	writeFailure(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "API route was not found")
	return nil
}
