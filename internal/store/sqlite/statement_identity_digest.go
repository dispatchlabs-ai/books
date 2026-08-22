package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"hash"
)

type statementIdentityRowsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// ActiveStatementAccountIdentityDigest returns a canonical binding for the
// complete active identity set of one statement account. Every immutable
// mapping and evidence field is length-prefixed before hashing so field values
// cannot create ambiguous encodings.
func ActiveStatementAccountIdentityDigest(ctx context.Context, q statementIdentityRowsQueryer, statementAccountID string) (int, string, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, source_system, source_realm, external_id,
		account_number, account_name, source_active, evidence_source_kind,
		evidence_source_path, evidence_source_sha256, evidence_locator,
		COALESCE(evidence_payload_sha256, ''), created_at, created_by
		FROM statement_account_identities
		WHERE statement_account_id = ? AND source_active = 1
		ORDER BY source_system, source_realm, external_id, id`, statementAccountID)
	if err != nil {
		return 0, "", MapError("read active statement-account identities", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(rows)

	digest := sha256.New()
	_, _ = digest.Write([]byte("books/statement-account-active-identity-set/v1\x00"))
	count := 0
	for rows.Next() {
		var id, sourceSystem, sourceRealm, externalID, accountNumber, accountName string
		var sourceActive int
		var evidenceSourceKind, evidenceSourcePath, evidenceSourceSHA256, evidenceLocator string
		var evidencePayloadSHA256, createdAt, createdBy string
		if err := rows.Scan(
			&id, &sourceSystem, &sourceRealm, &externalID,
			&accountNumber, &accountName, &sourceActive, &evidenceSourceKind,
			&evidenceSourcePath, &evidenceSourceSHA256, &evidenceLocator,
			&evidencePayloadSHA256, &createdAt, &createdBy,
		); err != nil {
			return 0, "", MapError("scan active statement-account identity", err)
		}
		for _, field := range []string{
			id, sourceSystem, sourceRealm, externalID, accountNumber, accountName,
			evidenceSourceKind, evidenceSourcePath, evidenceSourceSHA256,
			evidenceLocator, evidencePayloadSHA256, createdAt, createdBy,
		} {
			writeIdentityDigestField(digest, field)
		}
		var active [8]byte
		binary.BigEndian.PutUint64(active[:], uint64(sourceActive))
		_, _ = digest.Write(active[:])
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, "", MapError("read active statement-account identities", err)
	}
	return count, hex.EncodeToString(digest.Sum(nil)), nil
}

func writeIdentityDigestField(digest hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write([]byte(value))
}
