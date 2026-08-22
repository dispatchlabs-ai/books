package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	"github.com/dispatchlabs-ai/books/internal/version"
)

const genesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

type AuditInput struct {
	Actor         string
	Command       string
	AggregateType string
	AggregateID   string
	Payload       any
}

type AuditEvent struct {
	Sequence      int64           `json:"sequence"`
	EventID       string          `json:"event_id"`
	OccurredAt    string          `json:"occurred_at"`
	Actor         string          `json:"actor"`
	AppVersion    string          `json:"app_version"`
	Command       string          `json:"command"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	Payload       json.RawMessage `json:"payload"`
	PreviousHash  string          `json:"previous_hash"`
	EventHash     string          `json:"event_hash"`
}

func AppendAudit(ctx context.Context, tx *sql.Tx, input AuditInput) (AuditEvent, error) {
	input.Actor = strings.TrimSpace(input.Actor)
	if input.Actor == "" {
		return AuditEvent{}, apperr.New(apperr.Invalid, "ACTOR_REQUIRED", "actor is required for every mutation")
	}
	payload, err := json.Marshal(input.Payload)
	if err != nil {
		return AuditEvent{}, fmt.Errorf("encode audit payload: %w", err)
	}
	previousHash := genesisHash
	if err := tx.QueryRowContext(ctx, "SELECT event_hash FROM audit_events ORDER BY sequence DESC LIMIT 1").Scan(&previousHash); err != nil && err != sql.ErrNoRows {
		return AuditEvent{}, mapSQLiteError("read audit chain", err)
	}
	eventID, err := NewID()
	if err != nil {
		return AuditEvent{}, err
	}
	event := AuditEvent{
		EventID: eventID, OccurredAt: UTCNow(), Actor: input.Actor, AppVersion: version.Identifier(),
		Command: input.Command, AggregateType: input.AggregateType, AggregateID: input.AggregateID,
		Payload: payload, PreviousHash: previousHash,
	}
	event.EventHash = hashAudit(event)
	result, err := tx.ExecContext(ctx, `INSERT INTO audit_events
        (event_id, occurred_at, actor, app_version, command, aggregate_type, aggregate_id, payload_json, previous_hash, event_hash)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.EventID, event.OccurredAt, event.Actor, event.AppVersion, event.Command,
		event.AggregateType, event.AggregateID, string(event.Payload), event.PreviousHash, event.EventHash)
	if err != nil {
		return AuditEvent{}, mapSQLiteError("append audit event", err)
	}
	event.Sequence, err = result.LastInsertId()
	if err != nil {
		return AuditEvent{}, err
	}
	return event, nil
}

func hashAudit(event AuditEvent) string {
	canonical := strings.Join([]string{
		event.PreviousHash, event.EventID, event.OccurredAt, event.Actor, event.AppVersion,
		event.Command, event.AggregateType, event.AggregateID, string(event.Payload),
	}, "\x1f")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

type auditQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func VerifyAudit(ctx context.Context, db auditQueryer) (int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT sequence, event_id, occurred_at, actor, app_version,
        command, aggregate_type, aggregate_id, payload_json, previous_hash, event_hash
        FROM audit_events ORDER BY sequence`)
	if err != nil {
		return 0, mapSQLiteError("read audit chain", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)
	previous := genesisHash
	var count int64
	for rows.Next() {
		var event AuditEvent
		var payload string
		if err := rows.Scan(&event.Sequence, &event.EventID, &event.OccurredAt, &event.Actor, &event.AppVersion,
			&event.Command, &event.AggregateType, &event.AggregateID, &payload, &event.PreviousHash, &event.EventHash); err != nil {
			return count, err
		}
		event.Payload = json.RawMessage(payload)
		if event.PreviousHash != previous || event.EventHash != hashAudit(event) {
			return count, apperr.New(apperr.Integrity, "AUDIT_CHAIN_INVALID", fmt.Sprintf("audit chain failed at sequence %d", event.Sequence))
		}
		previous = event.EventHash
		count++
	}
	return count, rows.Err()
}
