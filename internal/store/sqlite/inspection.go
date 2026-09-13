package sqlite

import (
	"context"
	"encoding/json"
)

type AuditEventRecord struct {
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

func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEventRecord, error) {
	rowsDB, err := s.DB().QueryContext(ctx, `SELECT sequence, event_id, occurred_at, actor,
                app_version, command, aggregate_type, aggregate_id, payload_json, previous_hash, event_hash
                FROM audit_events ORDER BY sequence DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rowsDB)

	var data []AuditEventRecord
	for rowsDB.Next() {
		var value AuditEventRecord
		var payload string
		if err := rowsDB.Scan(&value.Sequence, &value.EventID, &value.OccurredAt, &value.Actor, &value.AppVersion, &value.Command, &value.AggregateType, &value.AggregateID, &payload, &value.PreviousHash, &value.EventHash); err != nil {
			return nil, err
		}
		value.Payload = json.RawMessage(payload)
		data = append(data, value)

	}
	return data, rowsDB.Err()
}

type DatabaseInfo struct {
	DatabaseID     string `json:"database_id"`
	CreatedAt      string `json:"created_at"`
	BaseCurrency   string `json:"base_currency"`
	SQLiteVersion  string `json:"sqlite_version"`
	MigrationCount int    `json:"migration_count"`
}

func (s *Store) DatabaseInfo(ctx context.Context) (DatabaseInfo, error) {
	var result DatabaseInfo
	if err := s.DB().QueryRowContext(ctx, `SELECT database_uuid, created_at, base_currency FROM database_metadata WHERE singleton=1`).Scan(&result.DatabaseID, &result.CreatedAt, &result.BaseCurrency); err != nil {
		return result, err
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT sqlite_version()`).Scan(&result.SQLiteVersion); err != nil {
		return result, err
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&result.MigrationCount); err != nil {
		return result, err
	}
	return result, nil
}
