package importer

import (
	"context"
	"fmt"
	"sort"
)

// Build reads immutable sources and returns a database-independent migration
// plan. Source content problems are diagnostics; filesystem and schema failures
// are returned as errors because a complete plan cannot be established.
func Build(ctx context.Context, input Request) (Plan, error) {
	request := cloneRequest(input)
	if err := validateRequest(request); err != nil {
		return Plan{}, err
	}
	states := make([]*entityState, 0, len(request.Entities))
	for _, entity := range request.Entities {
		if err := ctx.Err(); err != nil {
			return Plan{}, err
		}
		catalog, err := loadAccountCatalog(entity)
		if err != nil {
			return Plan{}, err
		}
		state := &entityState{request: entity, catalog: catalog}
		sources := append([]Source(nil), entity.Sources...)
		sort.Slice(sources, func(i, j int) bool {
			if sources[i].StartDate != sources[j].StartDate {
				return sources[i].StartDate < sources[j].StartDate
			}
			if sources[i].EndDate != sources[j].EndDate {
				return sources[i].EndDate < sources[j].EndDate
			}
			if sources[i].Path != sources[j].Path {
				return sources[i].Path < sources[j].Path
			}
			return sources[i].Kind < sources[j].Kind
		})
		for _, source := range sources {
			if err := ctx.Err(); err != nil {
				return Plan{}, err
			}
			switch source.Kind {
			case SourceJournalXLSX:
				err = parseJournalXLSX(state, source)
			case SourceGeneralLedger:
				err = parseGeneralLedger(state, source)
			case SourceQBOObjectDir:
				err = parseQBOObjectDirectory(state, source)
			default:
				return Plan{}, fmt.Errorf("entity %s has unsupported source kind %q", entity.EntityCode, source.Kind)
			}
			if err != nil {
				return Plan{}, fmt.Errorf("entity %s source %s: %w", entity.EntityCode, source.Path, err)
			}
		}
		deduplicateEntityJournals(state)
		states = append(states, state)
	}

	accounts, codeByKey, accountDiagnostics, err := reconcileAccounts(states)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Accounts: accounts, Diagnostics: accountDiagnostics}
	for _, state := range states {
		entityPlan := EntityPlan{EntityCode: state.request.EntityCode, BookCode: state.request.BookCode}
		for _, journal := range state.journals {
			entityPlan.Journals = append(entityPlan.Journals, journal.materialize(codeByKey))
		}
		plan.Entities = append(plan.Entities, entityPlan)
		plan.Diagnostics = append(plan.Diagnostics, state.diagnostics...)
	}
	sort.Slice(plan.Entities, func(i, j int) bool { return plan.Entities[i].EntityCode < plan.Entities[j].EntityCode })
	sortDiagnostics(plan.Diagnostics)
	return plan, nil
}

func cloneRequest(input Request) Request {
	result := Request{Entities: make([]EntityRequest, len(input.Entities))}
	for index, entity := range input.Entities {
		result.Entities[index] = entity
		result.Entities[index].Sources = append([]Source(nil), entity.Sources...)
	}
	return result
}

func deduplicateEntityJournals(state *entityState) {
	sort.Slice(state.journals, func(i, j int) bool {
		a, b := state.journals[i], state.journals[j]
		if a.sourceKey != b.sourceKey {
			return a.sourceKey < b.sourceKey
		}
		if a.evidence.SourcePath != b.evidence.SourcePath {
			return a.evidence.SourcePath < b.evidence.SourcePath
		}
		return a.evidence.Locator < b.evidence.Locator
	})
	result := make([]rawJournal, 0, len(state.journals))
	for index := 0; index < len(state.journals); {
		end := index + 1
		for end < len(state.journals) && state.journals[end].sourceKey == state.journals[index].sourceKey {
			end++
		}
		group := state.journals[index:end]
		firstDigest := rawJournalDigest(group[0])
		conflict := false
		for _, journal := range group[1:] {
			if rawJournalDigest(journal) != firstDigest {
				conflict = true
				break
			}
		}
		if conflict {
			for _, journal := range group {
				appendDiagnostic(&state.diagnostics, SeverityError, "SOURCE_KEY_CONFLICT", state.request,
					journal.evidence.SourcePath, journal.evidence.Locator, journal.sourceKey,
					"the same stable source key produced different journal content; every conflicting draft was withheld")
			}
		} else {
			result = append(result, group[0])
			for _, duplicate := range group[1:] {
				appendDiagnostic(&state.diagnostics, SeverityInfo, "SOURCE_DUPLICATE_IGNORED", state.request,
					duplicate.evidence.SourcePath, duplicate.evidence.Locator, duplicate.sourceKey,
					"identical source content was already planned for this entity")
			}
		}
		index = end
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].postingDate != result[j].postingDate {
			return result[i].postingDate < result[j].postingDate
		}
		return result[i].sourceKey < result[j].sourceKey
	})
	state.journals = result
}
