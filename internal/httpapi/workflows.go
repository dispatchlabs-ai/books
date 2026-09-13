package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/application"
)

// Workflow routes call the same company-bound operations as the local CLI.
// Registry creation, restore and local-path imports are local administration.
func serveWorkflow(w http.ResponseWriter, r *http.Request, p Principal, company string, app *application.Service, path []string) (bool, error) {
	ctx := r.Context()
	resource := strings.Join(path, "/")
	respond := func(value any, err error) (bool, error) {
		if err == nil {
			writeWorkflowData(w, http.StatusOK, value)
		}
		return true, err
	}
	require := func(permission string) bool {
		if granted(p, company, permission) {
			return true
		}
		writeFailure(w, http.StatusForbidden, "PERMISSION_DENIED", permission+" permission is required")
		return false
	}
	if r.Method == http.MethodGet {
		switch resource {
		case "dashboard":
			v, e := app.Dashboard(ctx)
			return respond(v, e)
		case "periods":
			v, e := app.Periods(ctx)
			return respond(v, e)
		case "defaults":
			v, e := app.AccountDefaults()
			return respond(v, e)
		case "reconciliations":
			q := r.URL.Query()
			v, e := app.Reconciliations(ctx, q.Get("account"), q.Get("status"), q.Get("from"), q.Get("to"))
			return respond(v, e)
		}
		if len(path) == 2 && path[0] == "transactions" {
			n, e := transactionNumber(path[1])
			if e != nil {
				return true, e
			}
			v, e := app.JournalByNumber(ctx, n)
			return respond(v, e)
		}
		if len(path) == 2 && path[0] == "reconciliations" {
			v, e := app.Reconciliation(ctx, path[1])
			return respond(v, e)
		}
		return false, nil
	}
	if r.Method != http.MethodPost {
		return false, nil
	}
	switch resource {
	case "transactions/spend", "transactions/receive", "transactions/transfer":
		if !require("post") {
			return true, nil
		}
		var input application.TransactionRequest
		if e := readWorkflowJSON(w, r, &input); e != nil {
			return true, e
		}
		key, e := workflowKey(r)
		if e != nil {
			return true, e
		}
		if input.Key != "" && input.Key != key {
			return true, apperr.New(apperr.Invalid, "IDEMPOTENCY_KEY_MISMATCH", "body key must match Idempotency-Key")
		}
		input.Key = key
		v, e := app.RecordTransaction(ctx, path[1], input)
		return respond(v, e)
	case "journals":
		if !require("post") {
			return true, nil
		}
		var input struct {
			Journal application.JournalInput `json:"journal"`
			Draft   bool                     `json:"draft"`
			DryRun  bool                     `json:"dry_run"`
		}
		if e := readWorkflowJSON(w, r, &input); e != nil {
			return true, e
		}
		key, e := workflowKey(r)
		if e != nil {
			return true, e
		}
		if e = manualJournal(&input.Journal, key); e != nil {
			return true, e
		}
		v, e := app.AddJournal(ctx, input.Journal, input.Draft, input.DryRun)
		return respond(v, e)
	case "accounts":
		if !require("manage") {
			return true, nil
		}
		var input application.AccountRequest
		if e := readWorkflowJSON(w, r, &input); e != nil {
			return true, e
		}
		if strings.TrimSpace(input.Code) == "" || input.ActiveFrom == "" {
			return true, apperr.New(apperr.Invalid, "ACCOUNT_INPUT_REQUIRED", "API account creation requires an explicit code and active_from date")
		}
		v, e := app.AddAccount(ctx, input)
		return respond(v, e)
	case "defaults":
		if !require("manage") {
			return true, nil
		}
		var input struct {
			Key     string `json:"key"`
			Account string `json:"account"`
		}
		if e := readWorkflowJSON(w, r, &input); e != nil {
			return true, e
		}
		if _, e := app.SetAccountDefault(ctx, input.Key, input.Account); e != nil {
			return true, e
		}
		v, e := app.AccountDefaults()
		return respond(v, e)
	case "periods":
		if !require("manage") {
			return true, nil
		}
		var input struct {
			FiscalYear int  `json:"fiscal_year"`
			DryRun     bool `json:"dry_run"`
		}
		if e := readWorkflowJSON(w, r, &input); e != nil {
			return true, e
		}
		v, e := app.AddFiscalYear(ctx, input.FiscalYear, input.DryRun)
		return respond(v, e)
	case "reconciliations/plan":
		var input application.ReconciliationRequest
		if e := readWorkflowJSON(w, r, &input); e != nil {
			return true, e
		}
		v, e := app.PlanReconciliation(ctx, input)
		return respond(v, e)
	case "reconciliations/apply":
		if !require("post") {
			return true, nil
		}
		var input struct {
			Plan   application.ReconciliationPlan `json:"plan"`
			DryRun bool                           `json:"dry_run"`
		}
		if e := readWorkflowJSON(w, r, &input); e != nil {
			return true, e
		}
		v, e := app.ApplyReconciliation(ctx, input.Plan, "manual-reconciliation-"+input.Plan.Digest+".json", input.DryRun)
		return respond(v, e)
	case "close/plan":
		var input struct {
			Period string `json:"period"`
		}
		if e := readWorkflowJSON(w, r, &input); e != nil {
			return true, e
		}
		v, e := app.PlanPeriodClose(ctx, input.Period)
		return respond(v, e)
	case "close/apply":
		if !require("manage") {
			return true, nil
		}
		var input struct {
			Plan   application.PeriodClosePlan `json:"plan"`
			DryRun bool                        `json:"dry_run"`
		}
		if e := readWorkflowJSON(w, r, &input); e != nil {
			return true, e
		}
		v, e := app.ApplyPeriodClose(ctx, input.Plan, input.DryRun)
		return respond(v, e)
	case "year-close/plan":
		var input struct {
			FiscalYear       int    `json:"fiscal_year"`
			RetainedEarnings string `json:"retained_earnings"`
		}
		if e := readWorkflowJSON(w, r, &input); e != nil {
			return true, e
		}
		v, e := app.PlanYearClose(ctx, input.FiscalYear, input.RetainedEarnings)
		return respond(v, e)
	case "year-close/apply":
		if !require("manage") || !require("post") {
			return true, nil
		}
		var input struct {
			Plan   application.YearClosePlan `json:"plan"`
			DryRun bool                      `json:"dry_run"`
		}
		if e := readWorkflowJSON(w, r, &input); e != nil {
			return true, e
		}
		v, e := app.ApplyYearClose(ctx, input.Plan, input.DryRun)
		return respond(v, e)
	}
	if len(path) == 3 && path[0] == "transactions" {
		switch path[2] {
		case "post", "abandon", "reverse", "undo", "correct":
		default:
			return false, nil
		}
		if !require("post") {
			return true, nil
		}
		n, e := transactionNumber(path[1])
		if e != nil {
			return true, e
		}
		var allowedKinds []string
		if !granted(p, company, "manage") {
			allowedKinds = []string{"STANDARD"}
		}
		original, e := app.JournalByNumber(ctx, n)
		if e != nil {
			return true, e
		}
		if (original.Kind == "CLOSING" || original.Kind == "CLOSING_REVERSAL") && !require("manage") {
			return true, nil
		}
		switch path[2] {
		case "post", "abandon":
			var input struct {
				DryRun bool `json:"dry_run"`
			}
			if e := readWorkflowJSON(w, r, &input); e != nil {
				return true, e
			}
			v, e := app.ChangeTransactionStatus(ctx, n, path[2], input.DryRun, allowedKinds...)
			return respond(v, e)
		case "reverse", "undo":
			var input struct {
				Date        string `json:"date"`
				Description string `json:"description"`
				Draft       bool   `json:"draft"`
				DryRun      bool   `json:"dry_run"`
			}
			if e := readWorkflowJSON(w, r, &input); e != nil {
				return true, e
			}
			v, e := app.ReverseTransaction(ctx, n, input.Date, input.Description, input.Draft, path[2] == "undo", input.DryRun, allowedKinds...)
			return respond(v, e)
		case "correct":
			var input struct {
				Journal application.JournalInput `json:"journal"`
				Reason  string                   `json:"reason"`
				Draft   bool                     `json:"draft"`
				DryRun  bool                     `json:"dry_run"`
			}
			if e := readWorkflowJSON(w, r, &input); e != nil {
				return true, e
			}
			if e := manualJournal(&input.Journal, ""); e != nil {
				return true, e
			}
			v, e := app.CorrectTransaction(ctx, n, input.Journal, input.Reason, input.Draft, input.DryRun, allowedKinds...)
			return respond(v, e)
		}
	}
	if len(path) == 3 && path[2] == "reopen" {
		if path[0] != "periods" && path[0] != "reconciliations" {
			return false, nil
		}
		if !require("manage") {
			return true, nil
		}
		var input struct {
			Reason string `json:"reason"`
			DryRun bool   `json:"dry_run"`
		}
		if e := readWorkflowJSON(w, r, &input); e != nil {
			return true, e
		}
		if path[0] == "periods" {
			v, e := app.ReopenPeriod(ctx, path[1], input.Reason, input.DryRun)
			return respond(v, e)
		}
		if input.DryRun {
			return true, apperr.New(apperr.Invalid, "DRY_RUN_UNSUPPORTED", "reconciliation reopen does not support dry_run")
		}
		v, e := app.ReopenReconciliation(ctx, path[1], input.Reason)
		return respond(v, e)
	}
	return false, nil
}

func transactionNumber(value string) (int64, error) {
	number, e := strconv.ParseInt(value, 10, 64)
	if e != nil || number < 1 || strconv.FormatInt(number, 10) != value {
		return 0, apperr.New(apperr.Invalid, "TRANSACTION_NUMBER_INVALID", "transaction number must be a positive decimal integer")
	}
	return number, nil
}
func workflowKey(r *http.Request) (string, error) {
	key := r.Header.Get("Idempotency-Key")
	if len(r.Header.Values("Idempotency-Key")) != 1 || len(key) < 1 || len(key) > 128 {
		return "", apperr.New(apperr.Invalid, "IDEMPOTENCY_KEY_INVALID", "provide one Idempotency-Key of 1 to 128 printable ASCII characters without spaces")
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return "", apperr.New(apperr.Invalid, "IDEMPOTENCY_KEY_INVALID", "idempotency key must contain printable ASCII without spaces")
		}
	}
	return key, nil
}
func manualJournal(input *application.JournalInput, key string) error {
	return application.PrepareManualJournal(input, key)
}
