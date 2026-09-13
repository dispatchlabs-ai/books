package application

import (
	"context"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"
	"strconv"
	"strings"
	"time"
)

type accountKind struct {
	Type             string
	Subtype          string
	Section          string
	StatementKind    string
	FirstCode        int
	LastCode         int
	PaymentCandidate bool
	DepositCandidate bool
}

var accountKinds = map[string]accountKind{
	"bank":        {Type: "ASSET", Subtype: "BANK", Section: "BALANCE_SHEET", StatementKind: "BANK", FirstCode: 1000, LastCode: 1090, PaymentCandidate: true, DepositCandidate: true},
	"ar":          {Type: "ASSET", Subtype: "ACCOUNTS_RECEIVABLE", Section: "BALANCE_SHEET", FirstCode: 1100, LastCode: 1190},
	"asset":       {Type: "ASSET", Subtype: "OTHER_ASSET", Section: "BALANCE_SHEET", FirstCode: 1200, LastCode: 1490},
	"fixed-asset": {Type: "ASSET", Subtype: "FIXED_ASSET", Section: "BALANCE_SHEET", FirstCode: 1500, LastCode: 1590},
	"investment":  {Type: "ASSET", Subtype: "INVESTMENT", Section: "BALANCE_SHEET", StatementKind: "INVESTMENT", FirstCode: 1600, LastCode: 1690},
	"ap":          {Type: "LIABILITY", Subtype: "ACCOUNTS_PAYABLE", Section: "BALANCE_SHEET", FirstCode: 2000, LastCode: 2090},
	"credit-card": {Type: "LIABILITY", Subtype: "CREDIT_CARD", Section: "BALANCE_SHEET", StatementKind: "CREDIT_CARD", FirstCode: 2100, LastCode: 2190, PaymentCandidate: true},
	"loan":        {Type: "LIABILITY", Subtype: "LOAN", Section: "BALANCE_SHEET", StatementKind: "LOAN", FirstCode: 2200, LastCode: 2290},
	"liability":   {Type: "LIABILITY", Subtype: "OTHER_LIABILITY", Section: "BALANCE_SHEET", FirstCode: 2300, LastCode: 2990},
	"equity":      {Type: "EQUITY", Subtype: "OWNER_EQUITY", Section: "BALANCE_SHEET", FirstCode: 3000, LastCode: 3990},
	"income":      {Type: "REVENUE", Subtype: "OPERATING_REVENUE", Section: "INCOME_STATEMENT", FirstCode: 4000, LastCode: 4990},
	"expense":     {Type: "EXPENSE", Subtype: "OPERATING_EXPENSE", Section: "INCOME_STATEMENT", FirstCode: 5000, LastCode: 9990},
}

type AccountResult struct {
	Code                       string `json:"code"`
	Name                       string `json:"name"`
	Kind                       string `json:"kind"`
	Type                       string `json:"type"`
	Subtype                    string `json:"subtype"`
	Book                       string `json:"book"`
	ActiveFrom                 string `json:"active_from"`
	StatementAccount           string `json:"statement_account,omitempty"`
	ReconciliationRequiredFrom string `json:"reconciliation_required_from,omitempty"`
	DefaultPayment             bool   `json:"default_payment"`
	DefaultDeposit             bool   `json:"default_deposit"`
	DefaultRetainedEarnings    bool   `json:"default_retained_earnings"`
	DryRun                     bool   `json:"dry_run"`
}

func AccountKind(account ledger.Account) string {
	for name, kind := range accountKinds {
		if account.Type == kind.Type && NormalizedSubtype(account.Subtype) == NormalizedSubtype(kind.Subtype) {
			return name
		}
	}
	return strings.ToLower(account.Type)
}

func nextAccountCode(accounts []ledger.Account, kind accountKind) (string, error) {
	used := make(map[int]bool, len(accounts))
	for _, account := range accounts {
		if value, err := strconv.Atoi(account.Code); err == nil {
			used[value] = true
		}
	}
	for value := kind.FirstCode; value <= kind.LastCode; value += 10 {
		if !used[value] {
			return strconv.Itoa(value), nil
		}
	}
	return "", apperr.New(apperr.Conflict, "ACCOUNT_CODE_RANGE_FULL", "no automatic account code remains in this kind's range; pass --code")
}

type AccountRequest struct {
	Kind             string `json:"kind"`
	Name             string `json:"name"`
	Code             string `json:"code,omitempty"`
	ActiveFrom       string `json:"active_from,omitempty"`
	ReconcileFrom    string `json:"reconcile_from,omitempty"`
	Currency         string `json:"currency,omitempty"`
	NoReconcile      bool   `json:"no_reconcile"`
	DefaultPayment   bool   `json:"default_payment"`
	DefaultDeposit   bool   `json:"default_deposit"`
	RetainedEarnings bool   `json:"retained_earnings"`
	DryRun           bool   `json:"dry_run"`
}

func (s *Service) AddAccount(ctx context.Context, r AccountRequest) (AccountResult, error) {
	code, activeFrom, reconcileFrom, currency := r.Code, r.ActiveFrom, r.ReconcileFrom, r.Currency
	noReconcile, defaultPayment, defaultDeposit, retainedEarnings := r.NoReconcile, r.DefaultPayment, r.DefaultDeposit, r.RetainedEarnings
	kindName := strings.ToLower(strings.TrimSpace(r.Kind))
	kind, ok := accountKinds[kindName]
	if !ok {
		return AccountResult{}, apperr.New(apperr.Invalid, "ACCOUNT_KIND_INVALID", "kind must be bank, credit-card, income, expense, ar, ap, loan, fixed-asset, asset, liability, equity, or investment")
	}
	name := strings.TrimSpace(r.Name)
	if name == "" {
		return AccountResult{}, apperr.New(apperr.Invalid, "ACCOUNT_NAME_REQUIRED", "account name is required")
	}
	if defaultPayment && !kind.PaymentCandidate {
		return AccountResult{}, apperr.New(apperr.Invalid, "DEFAULT_PAYMENT_ACCOUNT_INVALID", "--default-payment requires a bank or credit-card account")
	}
	if defaultDeposit && !kind.DepositCandidate {
		return AccountResult{}, apperr.New(apperr.Invalid, "DEFAULT_DEPOSIT_ACCOUNT_INVALID", "--default-deposit requires a bank account")
	}
	if retainedEarnings && kind.Type != "EQUITY" {
		return AccountResult{}, apperr.New(apperr.Invalid, "RETAINED_EARNINGS_ACCOUNT_INVALID", "--retained-earnings requires an equity account")
	}
	resolved := s.resolved
	service := s.ledger()
	accounts, err := service.ListAccounts(ctx, resolved.Company.BookCode)
	if err != nil {

		return AccountResult{}, err
	}
	automaticCode := strings.TrimSpace(code) == ""
	if automaticCode {
		code, err = nextAccountCode(accounts, kind)
		if err != nil {

			return AccountResult{}, err
		}
	} else {
		code = strings.ToUpper(strings.TrimSpace(code))
		for _, account := range accounts {
			if strings.EqualFold(account.Code, code) {

				return AccountResult{}, apperr.New(apperr.Conflict, "ACCOUNT_EXISTS", fmt.Sprintf("account code %s already exists", code))
			}
		}
	}
	if activeFrom == "" {
		periods, err := service.ListPeriods(ctx, resolved.Company.BookCode)
		if err != nil {

			return AccountResult{}, err
		}
		if len(periods) == 0 {

			return AccountResult{}, apperr.New(apperr.Validation, "PERIODS_REQUIRED", "create at least one fiscal period before adding an account")
		}
		activeFrom = periods[0].StartDate
	}
	if parsed, parseErr := time.Parse("2006-01-02", activeFrom); parseErr != nil || parsed.Format("2006-01-02") != activeFrom {

		return AccountResult{}, apperr.New(apperr.Invalid, "ACTIVE_FROM_INVALID", "--active-from must be an ISO date")
	}
	if reconcileFrom == "" {
		reconcileFrom = activeFrom
	}
	if parsed, parseErr := time.Parse("2006-01-02", reconcileFrom); parseErr != nil || parsed.Format("2006-01-02") != reconcileFrom {

		return AccountResult{}, apperr.New(apperr.Invalid, "RECONCILE_FROM_INVALID", "--reconcile-from must be an ISO date")
	}
	if currency == "" {
		currency = resolved.Company.Currency
	}
	currency = money.NormalizeCurrency(currency)
	if !money.IsSupportedCurrency(currency) {

		return AccountResult{}, apperr.New(apperr.Invalid, "CURRENCY_NOT_SUPPORTED", "choose a supported ISO monetary currency")
	}
	statementCode := ""
	if kind.StatementKind != "" && !noReconcile {
		if currency != resolved.Company.Currency {

			return AccountResult{}, apperr.New(apperr.Validation, "STATEMENT_ACCOUNT_CURRENCY_MISMATCH", "statement account currency must equal the selected company's functional currency")
		}
		statementCode = resolved.Company.EntityCode + "-" + code
		if len(statementCode) > 64 {

			return AccountResult{}, apperr.New(apperr.Invalid, "STATEMENT_ACCOUNT_CODE_INVALID", "company and account codes combine to more than 64 characters; use a shorter --code")
		}
	} else {
		reconcileFrom = ""
	}
	subtype := kind.Subtype
	if retainedEarnings {
		subtype = "RETAINED_EARNINGS"
	}
	result := AccountResult{
		Code: code, Name: name, Kind: kindName, Type: kind.Type, Subtype: subtype,
		Book: resolved.Company.BookCode, ActiveFrom: activeFrom, StatementAccount: statementCode,
		ReconciliationRequiredFrom: reconcileFrom, DefaultPayment: defaultPayment,
		DefaultDeposit: defaultDeposit, DefaultRetainedEarnings: retainedEarnings, DryRun: r.DryRun,
	}
	if r.DryRun {

		return result, nil
	}

	createAccount := func(candidate string) error {
		accountInput := ledger.CreateAccountInput{
			Code: candidate, Name: name, Type: kind.Type, Subtype: subtype, StatementSection: kind.Section,
			BookCodes: []string{resolved.Company.BookCode}, ActiveFrom: activeFrom,
		}
		if kind.StatementKind != "" && !noReconcile {
			candidateStatement := resolved.Company.EntityCode + "-" + candidate
			if _, _, err := service.CreateAccountWithStatement(ctx, accountInput, ledger.CreateStatementAccountInput{
				Code: candidateStatement, Entity: resolved.Company.EntityCode, Book: resolved.Company.BookCode,
				GLAccount: candidate, Name: name, Kind: kind.StatementKind, Currency: currency,
				RequiredForClose: true, ReconciliationRequiredFrom: reconcileFrom,
			}); err != nil {
				return err
			}
			statementCode = candidateStatement
			return nil
		}
		_, err := service.CreateAccount(ctx, accountInput)
		return err
	}

	for {
		err = createAccount(code)
		if err == nil {
			break
		}
		if !automaticCode {
			return AccountResult{}, err
		}
		latest, listErr := service.ListAccounts(ctx, resolved.Company.BookCode)
		if listErr != nil {
			return AccountResult{}, err
		}
		collided := false
		for _, account := range latest {
			if account.Code == code {
				collided = true
				break
			}
		}
		if !collided {
			return AccountResult{}, err
		}
		code, listErr = nextAccountCode(latest, kind)
		if listErr != nil {
			return AccountResult{}, listErr
		}
	}
	result.Code = code
	result.StatementAccount = statementCode
	if err := s.updateAccountDefaults(code, defaultPayment, defaultDeposit, retainedEarnings); err != nil {
		return result, apperr.Wrap(apperr.Unavailable, "ACCOUNT_DEFAULTS_PARTIAL", fmt.Sprintf("account %s was created; retry setting its defaults through config set", code), err)
	}
	return result, nil
}

func (s *Service) updateAccountDefaults(code string, payment, deposit, retained bool) error {
	if !payment && !deposit && !retained {
		return nil
	}
	path, companyKey := s.resolved.ConfigPath, s.company.Key
	_, updateErr := booksconfig.Update(path, nil, func(value *booksconfig.Config, _ bool) error {
		company, ok := value.Companies[companyKey]
		if !ok {
			return apperr.New(apperr.NotFound, "COMPANY_NOT_FOUND", fmt.Sprintf("company %q is not registered", companyKey))
		}
		current, resolveErr := value.Resolve(path, companyKey)
		if resolveErr != nil {
			return resolveErr
		}
		if current.Database != s.resolved.Database || company.Currency != s.resolved.Company.Currency || company.DatabaseUUID != s.resolved.Company.DatabaseUUID || company.BookCode != s.company.Book || company.EntityCode != s.company.Entity {
			return apperr.New(apperr.Conflict, "COMPANY_DATABASE_MISMATCH", "company registration changed")
		}
		if payment {
			company.Defaults.PaymentAccount = code
		}
		if deposit {
			company.Defaults.DepositAccount = code
		}
		if retained {
			company.Defaults.RetainedEarnings = code
		}
		value.Companies[companyKey] = company
		return nil
	})
	if updateErr != nil {
		return updateErr
	}
	return nil
}
