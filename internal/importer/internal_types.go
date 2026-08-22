package importer

import (
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/ledger"
)

type accountRecord struct {
	key              string
	entityCode       string
	externalID       string
	accountNum       string
	name             string
	fullyQualified   string
	accountType      string
	subtype          string
	normalBalance    string
	statementSection string
	active           bool
	evidence         Evidence
}

type accountCatalog struct {
	entityCode    string
	byID          map[string]*accountRecord
	byInternalKey map[string]*accountRecord
	byNumber      map[string][]*accountRecord
	byName        map[string][]*accountRecord
}

type rawLine struct {
	accountKey  string
	description string
	debitCents  int64
	creditCents int64
}

type rawJournal struct {
	entityCode   string
	bookCode     string
	postingDate  string
	period       string
	description  string
	reference    string
	sourceSystem string
	sourceKey    string
	evidence     Evidence
	lines        []rawLine
	payload      json.RawMessage
}

type entityState struct {
	request     EntityRequest
	catalog     *accountCatalog
	journals    []rawJournal
	diagnostics []Diagnostic
}

func (j rawJournal) materialize(codeByKey map[string]string) PlannedJournal {
	lines := make([]ledger.JournalLineInput, 0, len(j.lines))
	for _, line := range j.lines {
		lines = append(lines, ledger.JournalLineInput{
			Account:     codeByKey[line.accountKey],
			Description: line.description,
			DebitCents:  line.debitCents,
			CreditCents: line.creditCents,
		})
	}
	return PlannedJournal{
		EntityCode: j.entityCode,
		Input: ledger.CreateJournalInput{
			Book:         j.bookCode,
			PostingDate:  j.postingDate,
			Period:       j.period,
			Description:  j.description,
			Reference:    j.reference,
			SourceSystem: j.sourceSystem,
			SourceKey:    j.sourceKey,
			Lines:        lines,
		},
		Evidence: j.evidence,
	}
}
