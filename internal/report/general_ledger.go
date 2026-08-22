package report

import (
	"context"

	"github.com/dispatchlabs-ai/books/internal/apperr"
)

func anyBookAmount(amounts centsByBook) bool {
	for _, cents := range amounts {
		if cents != 0 {
			return true
		}
	}
	return false
}

// GeneralLedger returns posted detail in deterministic accounting order. Its
// opening, running, and closing balances are signed debit-minus-credit cents.
func (s *Service) GeneralLedger(ctx context.Context, input GeneralLedgerInput) (GeneralLedgerReport, error) {
	return withReportSnapshot(ctx, s, func(snapshot *Service) (GeneralLedgerReport, error) {
		return snapshot.generalLedger(ctx, input)
	})
}

func (s *Service) generalLedger(ctx context.Context, input GeneralLedgerInput) (GeneralLedgerReport, error) {
	if err := validateRange(input.FromDate, input.ToDate); err != nil {
		return GeneralLedgerReport{}, err
	}
	scope, err := s.resolveRangeScope(ctx, input.Scope, input.FromDate, input.ToDate)
	if err != nil {
		return GeneralLedgerReport{}, err
	}
	// Consolidation boundary contributions are derived from each owned book's
	// complete balance, so validate that complete source history as well as the
	// postings dated within the ownership-derived consolidation interval.
	if err := s.verifyPostedJournalIntegrity(ctx, withoutConsolidationIntervals(scope), input.ToDate); err != nil {
		return GeneralLedgerReport{}, err
	}
	catalog, err := s.loadAccountCatalog(ctx, scope)
	if err != nil {
		return GeneralLedgerReport{}, err
	}
	accountCode := normalizeCode(input.AccountCode)
	if accountCode != "" {
		filtered := catalog[:0]
		for _, account := range catalog {
			if account.Code == accountCode {
				filtered = append(filtered, account)
			}
		}
		catalog = filtered
		if len(catalog) == 0 {
			return GeneralLedgerReport{}, apperr.New(apperr.NotFound, "REPORT_ACCOUNT_NOT_FOUND", "account is not enabled in the report scope")
		}
	}
	lines, err := s.loadPostedLines(ctx, scope, "", input.ToDate, accountCode)
	if err != nil {
		return GeneralLedgerReport{}, err
	}
	boundaryLines, err := s.loadConsolidationBoundaryLines(ctx, scope, input.ToDate, accountCode)
	if err != nil {
		return GeneralLedgerReport{}, err
	}
	lines = append(lines, boundaryLines...)
	sortPostingLines(lines)

	type ledgerBuild struct {
		Account Account
		Opening centsByBook
		Closing centsByBook
		Lines   []postingLine
	}
	builds := make(map[string]*ledgerBuild)
	for _, line := range lines {
		build := builds[line.Account.ID]
		if build == nil {
			build = &ledgerBuild{Account: line.Account, Opening: centsByBook{}, Closing: centsByBook{}}
			builds[line.Account.ID] = build
		}
		change := line.DebitCents - line.CreditCents
		value, err := checkedAdd(build.Closing[line.BookID], change)
		if err != nil {
			return GeneralLedgerReport{}, err
		}
		build.Closing[line.BookID] = value
		if line.PostingDate < input.FromDate {
			value, err := checkedAdd(build.Opening[line.BookID], change)
			if err != nil {
				return GeneralLedgerReport{}, err
			}
			build.Opening[line.BookID] = value
		} else {
			build.Lines = append(build.Lines, line)
		}
	}
	if input.IncludeZero || accountCode != "" {
		for _, account := range catalog {
			if builds[account.ID] == nil {
				builds[account.ID] = &ledgerBuild{Account: account, Opening: centsByBook{}, Closing: centsByBook{}}
			}
		}
	}

	report := GeneralLedgerReport{Scope: scope, FromDate: input.FromDate, ToDate: input.ToDate}
	ordered := make(map[string]*accountBalance, len(builds))
	for id, build := range builds {
		ordered[id] = &accountBalance{Account: build.Account}
	}
	for _, orderedAccount := range sortedBalances(ordered) {
		build := builds[orderedAccount.Account.ID]
		if !input.IncludeZero && accountCode == "" && len(build.Lines) == 0 && !anyBookAmount(build.Opening) {
			continue
		}
		opening, err := breakdown(scope, build.Opening)
		if err != nil {
			return GeneralLedgerReport{}, err
		}
		closing, err := breakdown(scope, build.Closing)
		if err != nil {
			return GeneralLedgerReport{}, err
		}
		accountReport := GeneralLedgerAccount{
			Account: build.Account, OpeningBalance: opening,
			ClosingBalance: closing,
		}
		running := accountReport.OpeningBalance.ConsolidatedCents
		for _, line := range build.Lines {
			change := line.DebitCents - line.CreditCents
			running, err = checkedAdd(running, change)
			if err != nil {
				return GeneralLedgerReport{}, err
			}
			accountReport.Lines = append(accountReport.Lines, GeneralLedgerLine{
				JournalID: line.JournalID, EntryNumber: line.EntryNumber, PostingDate: line.PostingDate,
				BookCode: line.BookCode, BookKind: line.BookKind, EntityCode: line.EntityCode,
				Reference: line.Reference, JournalDescription: line.JournalDescription,
				LineNumber: line.LineNumber, LineDescription: line.LineDescription,
				DebitCents: line.DebitCents, CreditCents: line.CreditCents,
				ChangeCents: change, RunningBalanceCents: running,
				Synthetic: line.SyntheticKind != "", SyntheticKind: line.SyntheticKind,
			})
		}
		report.Accounts = append(report.Accounts, accountReport)
	}
	return report, nil
}
