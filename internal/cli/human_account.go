package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"

	"github.com/spf13/cobra"
)

type humanAccountKind struct {
	Type             string
	Subtype          string
	Section          string
	StatementKind    string
	FirstCode        int
	LastCode         int
	PaymentCandidate bool
	DepositCandidate bool
}

var humanAccountKinds = map[string]humanAccountKind{
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

type humanAccountResult struct {
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

type humanAccountListItem struct {
	Code             string `json:"code"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	Type             string `json:"type"`
	Subtype          string `json:"subtype"`
	NormalBalance    string `json:"normal_balance"`
	StatementSection string `json:"statement_section"`
	PostingEnabled   bool   `json:"posting_enabled"`
	ActiveFrom       string `json:"active_from"`
	ActiveTo         string `json:"active_to,omitempty"`
}

func newAccountsCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "accounts",
		Short: "List the selected company's chart of accounts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			accounts, err := ledger.NewService(store, opts.actor).ListAccounts(cmd.Context(), resolved.Company.BookCode)
			if err != nil {
				return err
			}
			values := make([]humanAccountListItem, 0, len(accounts))
			rows := make([][]string, 0, len(accounts))
			for _, account := range accounts {
				postingEnabled := account.PostingEnabled != nil && *account.PostingEnabled
				posting := "no"
				if postingEnabled {
					posting = "yes"
				}
				kind := humanKindForAccount(account)
				values = append(values, humanAccountListItem{
					Code: account.Code, Name: account.Name, Kind: kind, Type: account.Type, Subtype: account.Subtype,
					NormalBalance: account.NormalBalance, StatementSection: account.StatementSection,
					PostingEnabled: postingEnabled, ActiveFrom: account.ActiveFrom, ActiveTo: account.ActiveTo,
				})
				rows = append(rows, []string{account.Code, account.Name, kind, account.Type, account.Subtype, posting, account.ActiveFrom})
			}
			return writeResult(cmd, opts.format, values,
				[]string{"CODE", "NAME", "KIND", "TYPE", "SUBTYPE", "POSTING", "ACTIVE FROM"}, rows)
		},
	}
}

func humanKindForAccount(account ledger.Account) string {
	for name, kind := range humanAccountKinds {
		if account.Type == kind.Type && normalizedSubtype(account.Subtype) == normalizedSubtype(kind.Subtype) {
			return name
		}
	}
	return strings.ToLower(account.Type)
}

func newHumanAccountAddCommand(opts *options) *cobra.Command {
	var code, activeFrom, reconcileFrom, currency string
	var noReconcile, defaultPayment, defaultDeposit, retainedEarnings bool
	command := &cobra.Command{
		Use:   "add KIND NAME...",
		Short: "Add an account using bookkeeping-friendly defaults",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			kindName := strings.ToLower(strings.TrimSpace(args[0]))
			kind, ok := humanAccountKinds[kindName]
			if !ok {
				return apperr.New(apperr.Invalid, "ACCOUNT_KIND_INVALID", "kind must be bank, credit-card, income, expense, ar, ap, loan, fixed-asset, asset, liability, equity, or investment")
			}
			name := strings.TrimSpace(strings.Join(args[1:], " "))
			if name == "" {
				return apperr.New(apperr.Invalid, "ACCOUNT_NAME_REQUIRED", "account name is required")
			}
			if defaultPayment && !kind.PaymentCandidate {
				return apperr.New(apperr.Invalid, "DEFAULT_PAYMENT_ACCOUNT_INVALID", "--default-payment requires a bank or credit-card account")
			}
			if defaultDeposit && !kind.DepositCandidate {
				return apperr.New(apperr.Invalid, "DEFAULT_DEPOSIT_ACCOUNT_INVALID", "--default-deposit requires a bank account")
			}
			if retainedEarnings && kind.Type != "EQUITY" {
				return apperr.New(apperr.Invalid, "RETAINED_EARNINGS_ACCOUNT_INVALID", "--retained-earnings requires an equity account")
			}
			resolved, err := opts.resolveCompany()
			if err != nil {
				return err
			}
			store, err := openRead(cmd, opts)
			if err != nil {
				return err
			}
			service := ledger.NewService(store, opts.actor)
			accounts, err := service.ListAccounts(cmd.Context(), resolved.Company.BookCode)
			if err != nil {
				_ = store.Close()
				return err
			}
			automaticCode := strings.TrimSpace(code) == ""
			if automaticCode {
				code, err = nextAccountCode(accounts, kind)
				if err != nil {
					_ = store.Close()
					return err
				}
			} else {
				code = strings.ToUpper(strings.TrimSpace(code))
				for _, account := range accounts {
					if strings.EqualFold(account.Code, code) {
						_ = store.Close()
						return apperr.New(apperr.Conflict, "ACCOUNT_EXISTS", fmt.Sprintf("account code %s already exists", code))
					}
				}
			}
			if activeFrom == "" {
				periods, err := service.ListPeriods(cmd.Context(), resolved.Company.BookCode)
				if err != nil {
					_ = store.Close()
					return err
				}
				if len(periods) == 0 {
					_ = store.Close()
					return apperr.New(apperr.Validation, "PERIODS_REQUIRED", "create at least one fiscal period before adding an account")
				}
				activeFrom = periods[0].StartDate
			}
			if parsed, parseErr := time.Parse("2006-01-02", activeFrom); parseErr != nil || parsed.Format("2006-01-02") != activeFrom {
				_ = store.Close()
				return apperr.New(apperr.Invalid, "ACTIVE_FROM_INVALID", "--active-from must be an ISO date")
			}
			if reconcileFrom == "" {
				reconcileFrom = activeFrom
			}
			if parsed, parseErr := time.Parse("2006-01-02", reconcileFrom); parseErr != nil || parsed.Format("2006-01-02") != reconcileFrom {
				_ = store.Close()
				return apperr.New(apperr.Invalid, "RECONCILE_FROM_INVALID", "--reconcile-from must be an ISO date")
			}
			if currency == "" {
				currency = resolved.Company.Currency
			}
			currency = money.NormalizeCurrency(currency)
			if !money.IsSupportedCurrency(currency) {
				_ = store.Close()
				return apperr.New(apperr.Invalid, "CURRENCY_NOT_SUPPORTED", "this release supports USD as its only functional currency")
			}
			statementCode := ""
			if kind.StatementKind != "" && !noReconcile {
				if currency != resolved.Company.Currency {
					_ = store.Close()
					return apperr.New(apperr.Validation, "STATEMENT_ACCOUNT_CURRENCY_MISMATCH", "statement account currency must equal the selected company's functional currency")
				}
				statementCode = resolved.Company.EntityCode + "-" + code
				if len(statementCode) > 64 {
					_ = store.Close()
					return apperr.New(apperr.Invalid, "STATEMENT_ACCOUNT_CODE_INVALID", "company and account codes combine to more than 64 characters; use a shorter --code")
				}
			} else {
				reconcileFrom = ""
			}
			subtype := kind.Subtype
			if retainedEarnings {
				subtype = "RETAINED_EARNINGS"
			}
			result := humanAccountResult{
				Code: code, Name: name, Kind: kindName, Type: kind.Type, Subtype: subtype,
				Book: resolved.Company.BookCode, ActiveFrom: activeFrom, StatementAccount: statementCode,
				ReconciliationRequiredFrom: reconcileFrom, DefaultPayment: defaultPayment,
				DefaultDeposit: defaultDeposit, DefaultRetainedEarnings: retainedEarnings, DryRun: opts.dryRun,
			}
			if opts.dryRun {
				_ = store.Close()
				return writeHumanAccountResult(cmd, opts, result)
			}
			_ = store.Close()
			store, err = openWrite(cmd, opts)
			if err != nil {
				return err
			}
			defer func(closer interface{ Close() error }) { _ = closer.Close() }(store)
			service = ledger.NewService(store, opts.actor)
			createAccount := func(candidate string) error {
				accountInput := ledger.CreateAccountInput{
					Code: candidate, Name: name, Type: kind.Type, Subtype: subtype, StatementSection: kind.Section,
					BookCodes: []string{resolved.Company.BookCode}, ActiveFrom: activeFrom,
				}
				if kind.StatementKind != "" && !noReconcile {
					candidateStatement := resolved.Company.EntityCode + "-" + candidate
					if _, _, err := service.CreateAccountWithStatement(cmd.Context(), accountInput, ledger.CreateStatementAccountInput{
						Code: candidateStatement, Entity: resolved.Company.EntityCode, Book: resolved.Company.BookCode,
						GLAccount: candidate, Name: name, Kind: kind.StatementKind, Currency: currency,
						RequiredForClose: true, ReconciliationRequiredFrom: reconcileFrom,
					}); err != nil {
						return err
					}
					statementCode = candidateStatement
					return nil
				}
				_, err := service.CreateAccount(cmd.Context(), accountInput)
				return err
			}
			for {
				err = createAccount(code)
				if err == nil {
					break
				}
				if !automaticCode {
					return err
				}
				latest, listErr := service.ListAccounts(cmd.Context(), resolved.Company.BookCode)
				if listErr != nil {
					return err
				}
				collided := false
				for _, account := range latest {
					if account.Code == code {
						collided = true
						break
					}
				}
				if !collided {
					return err
				}
				code, listErr = nextAccountCode(latest, kind)
				if listErr != nil {
					return listErr
				}
			}
			result.Code = code
			result.StatementAccount = statementCode
			if err := updateCompanyAccountDefaults(opts, resolved.Key, code, defaultPayment, defaultDeposit, retainedEarnings); err != nil {
				return err
			}
			return writeHumanAccountResult(cmd, opts, result)
		},
	}
	command.Flags().StringVar(&code, "code", "", "account code (automatically assigned when omitted)")
	command.Flags().StringVar(&activeFrom, "active-from", "", "first posting date (defaults to the first configured period)")
	command.Flags().StringVar(&reconcileFrom, "reconcile-from", "", "first required reconciliation date for statement accounts")
	command.Flags().StringVar(&currency, "currency", "", "statement currency (defaults to company currency)")
	command.Flags().BoolVar(&noReconcile, "no-reconcile", false, "do not create a statement/reconciliation account")
	command.Flags().BoolVar(&defaultPayment, "default-payment", false, "use this account when spend --from is omitted")
	command.Flags().BoolVar(&defaultDeposit, "default-deposit", false, "use this account when receive --to is omitted")
	command.Flags().BoolVar(&retainedEarnings, "retained-earnings", false, "use this equity account for year close")
	return command
}

func nextAccountCode(accounts []ledger.Account, kind humanAccountKind) (string, error) {
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

func writeHumanAccountResult(cmd *cobra.Command, opts *options, result humanAccountResult) error {
	return writeResult(cmd, opts.format, result,
		[]string{"CODE", "NAME", "KIND", "BOOK", "STATEMENT ACCOUNT", "ACTIVE FROM", "DRY RUN"},
		[][]string{{result.Code, result.Name, result.Kind, result.Book, result.StatementAccount, result.ActiveFrom, fmt.Sprint(result.DryRun)}})
}

func updateCompanyAccountDefaults(opts *options, companyKey, code string, payment, deposit, retained bool) error {
	if !payment && !deposit && !retained {
		return nil
	}
	path, err := opts.resolveConfigPath()
	if err != nil {
		return err
	}
	value, updateErr := booksconfig.Update(path, nil, func(value *booksconfig.Config, _ bool) error {
		company, ok := value.Companies[companyKey]
		if !ok {
			return apperr.New(apperr.NotFound, "COMPANY_NOT_FOUND", fmt.Sprintf("company %q is not registered", companyKey))
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
		return configMutationError("save account defaults", updateErr)
	}
	opts.loadedConfig = &value
	opts.resolved = nil
	return nil
}

func normalizeAccountSelector(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func normalizedSubtype(value string) string {
	return strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(value)), "-", "_")
}

func resolveHumanAccount(accounts []ledger.Account, selector string) (ledger.Account, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return ledger.Account{}, apperr.New(apperr.Invalid, "ACCOUNT_REQUIRED", "an account code or unambiguous name is required")
	}
	for _, account := range accounts {
		if strings.EqualFold(account.Code, selector) {
			return account, nil
		}
	}
	for _, account := range accounts {
		if strings.EqualFold(account.Name, selector) {
			return account, nil
		}
	}
	normalized := normalizeAccountSelector(selector)
	var matches []ledger.Account
	for _, account := range accounts {
		candidate := normalizeAccountSelector(account.Name)
		if normalized != "" && (candidate == normalized || strings.Contains(candidate, normalized)) {
			matches = append(matches, account)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		names := make([]string, 0, len(matches))
		for _, match := range matches {
			names = append(names, match.Code+" "+match.Name)
		}
		sort.Strings(names)
		return ledger.Account{}, apperr.New(apperr.Invalid, "ACCOUNT_AMBIGUOUS", fmt.Sprintf("account %q matches: %s; use a code", selector, strings.Join(names, ", ")))
	}
	return ledger.Account{}, apperr.New(apperr.NotFound, "ACCOUNT_NOT_FOUND", fmt.Sprintf("account %q was not found", selector))
}
