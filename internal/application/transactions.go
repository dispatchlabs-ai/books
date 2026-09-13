package application

import (
	"context"
	"fmt"
	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
	"github.com/dispatchlabs-ai/books/internal/ledger"
	"github.com/dispatchlabs-ai/books/internal/money"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type TransactionLine struct {
	Line        int    `json:"line"`
	Account     string `json:"account"`
	AccountName string `json:"account_name"`
	Description string `json:"description,omitempty"`
	DebitCents  int64  `json:"debit_cents"`
	CreditCents int64  `json:"credit_cents"`
}

type Transaction struct {
	Currency         money.Currency    `json:"currency"`
	Company          string            `json:"company"`
	Book             string            `json:"book"`
	Number           int64             `json:"number,omitempty"`
	Kind             string            `json:"kind"`
	Date             string            `json:"date"`
	Period           string            `json:"period"`
	Status           string            `json:"status"`
	Description      string            `json:"description"`
	Reference        string            `json:"reference,omitempty"`
	TotalDebitCents  int64             `json:"total_debit_cents"`
	TotalCreditCents int64             `json:"total_credit_cents"`
	Lines            []TransactionLine `json:"lines,omitempty"`
	DryRun           bool              `json:"dry_run"`
}

type Correction struct {
	Company        string      `json:"company"`
	OriginalNumber int64       `json:"original_number"`
	Reason         string      `json:"reason"`
	Reversal       Transaction `json:"reversal"`
	Replacement    Transaction `json:"replacement"`
	DryRun         bool        `json:"dry_run"`
}

type JournalInput struct {
	Book                string        `json:"book"`
	Kind                string        `json:"kind,omitempty"`
	PostingDate         string        `json:"posting_date"`
	Period              string        `json:"period"`
	Description         string        `json:"description"`
	Reference           string        `json:"reference,omitempty"`
	SourceSystem        string        `json:"source_system,omitempty"`
	SourceKey           string        `json:"source_key,omitempty"`
	TaxType             string        `json:"tax_type,omitempty"`
	TaxAccountingPeriod string        `json:"tax_accounting_period,omitempty"`
	Lines               []JournalLine `json:"lines"`
}

type JournalLine struct {
	Account            string `json:"account"`
	Description        string `json:"description,omitempty"`
	Debit              string `json:"debit,omitempty"`
	Credit             string `json:"credit,omitempty"`
	CounterpartyEntity string `json:"counterparty_entity,omitempty"`
	IntercompanyKey    string `json:"intercompany_key,omitempty"`
}

func (input JournalInput) LedgerInput(currencies ...money.Currency) (ledger.CreateJournalInput, error) {
	currency := money.Currency{}
	if len(currencies) > 0 {
		currency = currencies[0]
	}
	result := ledger.CreateJournalInput{
		Book: input.Book, Kind: input.Kind, PostingDate: input.PostingDate, Period: input.Period, Description: input.Description,
		Reference: input.Reference, SourceSystem: input.SourceSystem, SourceKey: input.SourceKey,
		TaxType: input.TaxType, TaxAccountingPeriod: input.TaxAccountingPeriod,
	}
	for i, line := range input.Lines {
		var debit, credit int64
		var err error
		if strings.TrimSpace(line.Debit) != "" {
			debit, err = currency.Parse(line.Debit)
			if err != nil {
				return result, apperr.Wrap(apperr.Input, "JOURNAL_AMOUNT_INVALID", fmt.Sprintf("line %d debit is invalid", i+1), err)
			}
		}
		if strings.TrimSpace(line.Credit) != "" {
			credit, err = currency.Parse(line.Credit)
			if err != nil {
				return result, apperr.Wrap(apperr.Input, "JOURNAL_AMOUNT_INVALID", fmt.Sprintf("line %d credit is invalid", i+1), err)
			}
		}
		result.Lines = append(result.Lines, ledger.JournalLineInput{Account: line.Account, Description: line.Description, DebitCents: debit, CreditCents: credit, CounterpartyEntity: line.CounterpartyEntity, IntercompanyKey: line.IntercompanyKey})
	}
	return result, nil
}

func positiveMoney(value string, currencies ...money.Currency) (int64, error) {
	currency := money.Currency{}
	if len(currencies) > 0 {
		currency = currencies[0]
	}
	amount, err := currency.Parse(value)
	if err != nil {
		return 0, apperr.Wrap(apperr.Invalid, "AMOUNT_INVALID", "amount must be an unformatted decimal such as 42.50", err)
	}
	if amount <= 0 {
		return 0, apperr.New(apperr.Invalid, "AMOUNT_INVALID", "amount must be greater than zero")
	}
	return amount, nil
}

func isBalanceSheetAccount(account ledger.Account) bool {
	switch account.Type {
	case "ASSET", "LIABILITY", "EQUITY":
		return true
	default:
		return false
	}
}

func setManualSource(input *ledger.CreateJournalInput, key string) {
	if strings.TrimSpace(key) != "" {
		input.SourceSystem = "MANUAL"
		input.SourceKey = strings.TrimSpace(key)
	}
}

func resolveDefaultTransactionAccount(accounts []ledger.Account, explicit, configured string, subtypes []string, flag, setting string) (ledger.Account, error) {
	allowed := make(map[string]bool, len(subtypes))
	for _, subtype := range subtypes {
		allowed[subtype] = true
	}
	resolveEligible := func(selector string) (ledger.Account, error) {
		account, err := ResolveAccount(accounts, selector)
		if err != nil {
			return ledger.Account{}, err
		}
		if !allowed[NormalizedSubtype(account.Subtype)] {
			return ledger.Account{}, apperr.New(apperr.Invalid, "TRANSACTION_ACCOUNT_KIND_INVALID", fmt.Sprintf("%s requires an eligible %s account", flag, strings.ToLower(strings.Join(subtypes, " or "))))
		}
		return account, nil
	}
	if strings.TrimSpace(explicit) != "" {
		return resolveEligible(explicit)
	}
	if strings.TrimSpace(configured) != "" {
		return resolveEligible(configured)
	}
	var candidates []ledger.Account
	for _, account := range accounts {
		if allowed[NormalizedSubtype(account.Subtype)] && account.PostingEnabled != nil && *account.PostingEnabled {
			candidates = append(candidates, account)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return ledger.Account{}, apperr.New(apperr.Validation, "DEFAULT_ACCOUNT_MISSING", fmt.Sprintf("no eligible account exists; pass %s or add a bank/card account", flag))
	}
	return ledger.Account{}, apperr.New(apperr.Invalid, "DEFAULT_ACCOUNT_AMBIGUOUS", fmt.Sprintf("more than one eligible account exists; pass %s or set %s", flag, setting))
}

func TransactionFromInput(company string, input ledger.CreateJournalInput, draft, dryRun bool, currency money.Currency) Transaction {
	status := "POSTED"
	if draft {
		status = "DRAFT"
	}
	if dryRun {
		status = "PREVIEW"
	}
	result := Transaction{Currency: currency, Company: company, Book: input.Book, Kind: input.Kind, Date: input.PostingDate, Period: input.Period, Status: status, Description: input.Description, Reference: input.Reference, DryRun: dryRun}
	if result.Kind == "" {
		result.Kind = "STANDARD"
	}
	for index, line := range input.Lines {
		result.Lines = append(result.Lines, TransactionLine{Line: index + 1, Account: line.Account, Description: line.Description, DebitCents: line.DebitCents, CreditCents: line.CreditCents})
		result.TotalDebitCents += line.DebitCents
		result.TotalCreditCents += line.CreditCents
	}
	return result
}

func TransactionFromJournal(company string, journal ledger.Journal) Transaction {
	result := Transaction{Currency: journal.Currency, Company: company, Book: journal.BookCode, Number: journal.EntryNumber, Kind: journal.Kind, Date: journal.PostingDate, Period: journal.PeriodCode, Status: journal.Status, Description: journal.Description, Reference: journal.Reference, TotalDebitCents: journal.TotalDebitCents, TotalCreditCents: journal.TotalCreditCents}
	for _, line := range journal.Lines {
		result.Lines = append(result.Lines, TransactionLine{Line: line.LineNumber, Account: line.AccountCode, AccountName: line.AccountName, Description: line.Description, DebitCents: line.DebitCents, CreditCents: line.CreditCents})
	}
	return result
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

func NormalizedSubtype(value string) string {
	return strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(value)), "-", "_")
}

func ResolveAccount(accounts []ledger.Account, selector string) (ledger.Account, error) {
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

// TransactionRequest describes a routine posting independently of any UI.
// Dates are explicit ISO dates; shell conveniences are resolved by the CLI.
type TransactionRequest struct {
	Amount      string `json:"amount"`
	Account     string `json:"account,omitempty"`
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
	Date        string `json:"date"`
	Description string `json:"description,omitempty"`
	Reference   string `json:"reference,omitempty"`
	Key         string `json:"key,omitempty"`
	Draft       bool   `json:"draft"`
	DryRun      bool   `json:"dry_run"`
}

type postingContext struct {
	currency money.Currency
	accounts []ledger.Account
	period   ledger.Period
	date     string
}

func (s *Service) settings() (booksconfig.Company, error) {
	cfg, err := booksconfig.Load(s.resolved.ConfigPath)
	if err != nil {
		return booksconfig.Company{}, err
	}
	resolved, err := cfg.Resolve(s.resolved.ConfigPath, s.company.Key)
	if err != nil {
		return booksconfig.Company{}, err
	}
	c := resolved.Company
	if resolved.Database != s.resolved.Database || c.DatabaseUUID != s.resolved.Company.DatabaseUUID || c.BookCode != s.company.Book || c.EntityCode != s.company.Entity || c.Currency != s.company.Currency {
		return booksconfig.Company{}, apperr.New(apperr.Conflict, "COMPANY_DATABASE_MISMATCH", "registered company identity changed; reopen the application")
	}
	return c, nil
}

func (s *Service) postingContext(ctx context.Context, date string) (postingContext, error) {
	var result postingContext
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || parsed.Format("2006-01-02") != date {
		return result, apperr.New(apperr.Invalid, "DATE_INVALID", "date must be YYYY-MM-DD")
	}
	result.date = date
	result.currency, err = money.Lookup(s.company.Currency)
	if err != nil {
		return result, err
	}
	result.accounts, err = s.Accounts(ctx)
	if err != nil {
		return result, err
	}
	periods, err := s.ledger().ListPeriods(ctx, s.company.Book)
	if err != nil {
		return result, err
	}
	for _, p := range periods {
		if p.StartDate <= date && date <= p.EndDate {
			result.period = p
			break
		}
	}
	if result.period.Code == "" {
		return result, apperr.New(apperr.Validation, "PERIOD_NOT_CONFIGURED", fmt.Sprintf("no fiscal period contains %s", date))
	}
	if result.period.BookStatus != "OPEN" {
		return result, apperr.New(apperr.Conflict, "PERIOD_CLOSED", fmt.Sprintf("period %s is %s", result.period.Code, result.period.BookStatus))
	}
	return result, nil
}

func (s *Service) RecordTransaction(ctx context.Context, kind string, r TransactionRequest) (Transaction, error) {
	p, err := s.postingContext(ctx, r.Date)
	if err != nil {
		return Transaction{}, err
	}
	amount, err := positiveMoney(r.Amount, p.currency)
	if err != nil {
		return Transaction{}, err
	}
	settings, err := s.settings()
	if err != nil {
		return Transaction{}, err
	}
	var debit, credit ledger.Account
	var fallback string
	switch kind {
	case "spend":
		debit, err = ResolveAccount(p.accounts, r.Account)
		if err != nil {
			return Transaction{}, err
		}
		credit, err = resolveDefaultTransactionAccount(p.accounts, r.From, settings.Defaults.PaymentAccount, []string{"BANK", "CREDIT_CARD"}, "from", "defaults.payment-account")
		fallback = "Spend: " + debit.Name
	case "receive":
		credit, err = ResolveAccount(p.accounts, r.Account)
		if err != nil {
			return Transaction{}, err
		}
		debit, err = resolveDefaultTransactionAccount(p.accounts, r.To, settings.Defaults.DepositAccount, []string{"BANK"}, "to", "defaults.deposit-account")
		fallback = "Receive: " + credit.Name
	case "transfer":
		credit, err = ResolveAccount(p.accounts, r.From)
		if err != nil {
			return Transaction{}, err
		}
		debit, err = ResolveAccount(p.accounts, r.To)
		if err == nil && (!isBalanceSheetAccount(debit) || !isBalanceSheetAccount(credit)) {
			return Transaction{}, apperr.New(apperr.Invalid, "TRANSFER_ACCOUNT_INVALID", "transfer requires two balance-sheet accounts (asset, liability, or equity)")
		}
		fallback = "Transfer: " + credit.Name + " to " + debit.Name
	default:
		return Transaction{}, apperr.New(apperr.Invalid, "TRANSACTION_KIND_INVALID", "transaction kind must be spend, receive, or transfer")
	}
	if err != nil {
		return Transaction{}, err
	}
	if debit.Code == credit.Code {
		return Transaction{}, apperr.New(apperr.Invalid, "TRANSACTION_ACCOUNTS_SAME", "transaction accounts must be different")
	}
	description := strings.TrimSpace(r.Description)
	if description == "" {
		description = fallback
	}
	input := ledger.CreateJournalInput{Book: s.company.Book, PostingDate: p.date, Period: p.period.Code, Description: description, Reference: r.Reference, Lines: []ledger.JournalLineInput{{Account: debit.Code, Description: description, DebitCents: amount}, {Account: credit.Code, Description: description, CreditCents: amount}}}
	setManualSource(&input, r.Key)
	return s.commitJournal(ctx, p, input, r.Draft, r.DryRun)
}

func (s *Service) prepareJournal(ctx context.Context, file JournalInput) (postingContext, ledger.CreateJournalInput, error) {
	p, err := s.postingContext(ctx, file.PostingDate)
	if err != nil {
		return p, ledger.CreateJournalInput{}, err
	}
	if file.Book != "" && !strings.EqualFold(file.Book, s.company.Book) {
		return p, ledger.CreateJournalInput{}, apperr.New(apperr.Invalid, "BOOK_INVALID", "journal must use the selected company's book")
	}
	file.Book = s.company.Book
	if file.Period != "" && !strings.EqualFold(file.Period, p.period.Code) {
		return p, ledger.CreateJournalInput{}, apperr.New(apperr.Invalid, "PERIOD_DATE_MISMATCH", fmt.Sprintf("%s belongs to period %s", p.date, p.period.Code))
	}
	file.Period = p.period.Code
	for i := range file.Lines {
		account, e := ResolveAccount(p.accounts, file.Lines[i].Account)
		if e != nil {
			return p, ledger.CreateJournalInput{}, apperr.Wrap(apperr.Invalid, "JOURNAL_ACCOUNT_INVALID", fmt.Sprintf("resolve line %d account", i+1), e)
		}
		file.Lines[i].Account = account.Code
	}
	input, err := file.LedgerInput(p.currency)
	if err != nil {
		return p, input, err
	}
	return p, input, s.validateJournal(p, input)
}

func (s *Service) AddJournal(ctx context.Context, file JournalInput, draft, dryRun bool) (Transaction, error) {
	p, input, err := s.prepareJournal(ctx, file)
	if err != nil {
		return Transaction{}, err
	}
	return s.commitJournal(ctx, p, input, draft, dryRun)
}

func (s *Service) validateJournal(p postingContext, input ledger.CreateJournalInput) error {
	if input.Book != s.company.Book || input.Period != p.period.Code || input.PostingDate != p.date {
		return apperr.New(apperr.Invalid, "JOURNAL_CONTEXT_INVALID", "journal book, period, and date must match the selected company posting context")
	}
	if strings.TrimSpace(input.Description) == "" || len(input.Lines) < 2 {
		return apperr.New(apperr.Invalid, "JOURNAL_INVALID", "description and at least two lines are required")
	}
	accounts := map[string]ledger.Account{}
	for _, a := range p.accounts {
		accounts[a.Code] = a
	}
	var debits, credits int64
	for i, line := range input.Lines {
		account, ok := accounts[strings.ToUpper(strings.TrimSpace(line.Account))]
		if !ok {
			return apperr.New(apperr.NotFound, "ACCOUNT_NOT_FOUND", fmt.Sprintf("journal line %d account %q was not found", i+1, line.Account))
		}
		if account.PostingEnabled == nil || !*account.PostingEnabled || p.date < account.ActiveFrom || (account.ActiveTo != "" && p.date > account.ActiveTo) {
			return apperr.New(apperr.Validation, "ACCOUNT_NOT_ACTIVE", fmt.Sprintf("account %s is not enabled for posting on %s", account.Code, p.date))
		}
		if line.DebitCents < 0 || line.CreditCents < 0 || (line.DebitCents > 0) == (line.CreditCents > 0) {
			return apperr.New(apperr.Invalid, "JOURNAL_LINE_INVALID", fmt.Sprintf("line %d must contain exactly one positive debit or credit", i+1))
		}
		if line.DebitCents > math.MaxInt64-debits || line.CreditCents > math.MaxInt64-credits {
			return apperr.New(apperr.Invalid, "JOURNAL_AMOUNT_OVERFLOW", "journal totals exceed integer minor-unit range")
		}
		debits += line.DebitCents
		credits += line.CreditCents
	}
	if debits == 0 || debits != credits {
		return apperr.New(apperr.Validation, "JOURNAL_UNBALANCED", fmt.Sprintf("debits %s must equal credits %s", p.currency.Format(debits), p.currency.Format(credits)))
	}
	return nil
}
func (s *Service) commitJournal(ctx context.Context, p postingContext, input ledger.CreateJournalInput, draft, dryRun bool) (Transaction, error) {
	if err := s.validateJournal(p, input); err != nil {
		return Transaction{}, err
	}
	if dryRun {
		return TransactionFromInput(s.company.Key, input, draft, true, p.currency), nil
	}
	var journal ledger.Journal
	var err error
	if draft {
		journal, err = s.ledger().CreateJournal(ctx, input)
	} else {
		journal, err = s.ledger().CreateAndPostJournal(ctx, input)
	}
	if err != nil {
		return Transaction{}, err
	}
	if journal.Status == "ABANDONED" {
		return Transaction{}, apperr.New(apperr.Conflict, "TRANSACTION_KEY_ABANDONED", "this idempotency key belongs to an abandoned transaction; use a new key for revised activity")
	}
	return TransactionFromJournal(s.company.Key, journal), nil
}

func (s *Service) JournalByNumber(ctx context.Context, number int64) (ledger.Journal, error) {
	if number < 1 {
		return ledger.Journal{}, apperr.New(apperr.Invalid, "TRANSACTION_NUMBER_INVALID", "transaction number must be a positive integer")
	}
	return s.ledger().GetJournalByNumber(ctx, s.company.Book, number)
}
func (s *Service) ChangeTransactionStatus(ctx context.Context, number int64, action string, dryRun bool, allowedKinds ...string) (Transaction, error) {
	if !dryRun {
		if number < 1 {
			return Transaction{}, apperr.New(apperr.Invalid, "TRANSACTION_NUMBER_INVALID", "transaction number must be positive")
		}
		j, err := s.ledger().ChangeJournalStatus(ctx, s.company.Book, number, action, allowedKinds...)
		if err != nil {
			return Transaction{}, err
		}
		return TransactionFromJournal(s.company.Key, j), nil
	}

	j, err := s.JournalByNumber(ctx, number)
	if err != nil {
		return Transaction{}, err
	}
	if err = ledger.CheckJournalKind(j.Kind, allowedKinds); err != nil {
		return Transaction{}, err
	}
	target := ""
	switch action {
	case "post":
		target = "POSTED"
	case "abandon":
		target = "ABANDONED"
	default:
		return Transaction{}, apperr.New(apperr.Invalid, "TRANSACTION_ACTION_INVALID", "action must be post or abandon")
	}
	if j.Status == target {
		return TransactionFromJournal(s.company.Key, j), nil
	}
	if j.Status != "DRAFT" {
		return Transaction{}, apperr.New(apperr.Conflict, "JOURNAL_NOT_DRAFT", "only a draft transaction can be posted or abandoned")
	}
	if dryRun {
		if action == "post" {
			v, e := s.ledger().ValidateJournal(ctx, j.ID)
			if e != nil {
				return Transaction{}, e
			}
			if !v.Valid {
				return Transaction{}, apperr.New(apperr.Validation, "JOURNAL_INVALID", strings.Join(v.Errors, "; "))
			}
		}
		result := TransactionFromJournal(s.company.Key, j)
		result.Status = strings.ToUpper(action) + " (PREVIEW)"
		result.DryRun = true
		return result, nil
	}
	return Transaction{}, apperr.New(apperr.Invalid, "TRANSACTION_ACTION_INVALID", "invalid transaction preview")
}

func (s *Service) CorrectTransaction(ctx context.Context, number int64, file JournalInput, reason string, draft, dryRun bool, allowedKinds ...string) (Correction, error) {
	original, err := s.JournalByNumber(ctx, number)
	if err != nil {
		return Correction{}, err
	}
	p, replacement, err := s.prepareJournal(ctx, file)
	if err != nil {
		return Correction{}, err
	}
	input := ledger.CorrectionInput{AllowedKinds: allowedKinds, OriginalID: original.ID, Replacement: replacement, Reason: reason, Draft: draft}
	reversal, replacement, err := s.ledger().PrepareCorrection(ctx, input)
	if err != nil {
		return Correction{}, err
	}
	if err = s.validateJournal(p, replacement); err != nil {
		return Correction{}, err
	}
	if err = s.validateJournal(p, reversal); err != nil {
		return Correction{}, err
	}
	result := Correction{Company: s.company.Key, OriginalNumber: number, Reason: strings.TrimSpace(reason), DryRun: dryRun}
	if dryRun {
		result.Reversal = TransactionFromInput(s.company.Key, reversal, draft, true, p.currency)
		result.Replacement = TransactionFromInput(s.company.Key, replacement, draft, true, p.currency)
		return result, nil
	}
	committed, err := s.ledger().CorrectJournal(ctx, input)
	if err != nil {
		return Correction{}, err
	}
	result.Reversal = TransactionFromJournal(s.company.Key, committed.Reversal)
	result.Replacement = TransactionFromJournal(s.company.Key, committed.Replacement)
	return result, nil
}

func (s *Service) ReverseTransaction(ctx context.Context, number int64, date, memo string, draft, undo, dryRun bool, allowedKinds ...string) (Transaction, error) {
	original, err := s.JournalByNumber(ctx, number)
	if err != nil {
		return Transaction{}, err
	}
	if err = ledger.CheckJournalKind(original.Kind, allowedKinds); err != nil {
		return Transaction{}, err
	}
	if undo && (original.Status == "DRAFT" || original.Status == "ABANDONED") {
		return s.ChangeTransactionStatus(ctx, number, "abandon", dryRun, allowedKinds...)
	}
	p, err := s.postingContext(ctx, date)
	if err != nil {
		return Transaction{}, err
	}
	if strings.TrimSpace(memo) == "" && undo {
		memo = "Undo transaction " + strconv.FormatInt(number, 10)
	}
	input, err := s.ledger().ReversalInput(ctx, original.ID, date, p.period.Code, memo)
	if err != nil {
		return Transaction{}, err
	}
	if err = s.validateJournal(p, input); err != nil {
		return Transaction{}, err
	}
	if dryRun {
		return TransactionFromInput(s.company.Key, input, draft, true, p.currency), nil
	}
	journal, err := s.ledger().ReverseAndRecordJournal(ctx, original.ID, date, p.period.Code, memo, draft, allowedKinds...)
	if err != nil {
		return Transaction{}, err
	}
	return TransactionFromJournal(s.company.Key, journal), nil
}

func (s *Service) ListTransactions(ctx context.Context, from, to, status string) ([]Transaction, error) {
	journals, err := s.ledger().ListJournals(ctx, s.company.Book, from, to, status)
	if err != nil {
		return nil, err
	}
	result := make([]Transaction, 0, len(journals))
	for _, j := range journals {
		result = append(result, TransactionFromJournal(s.company.Key, j))
	}
	return result, nil
}
