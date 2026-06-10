package backends

import (
	"context"
	"database/sql"

	account "github.com/jiujiu532/grok2api/app/control/account"
	platformruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func (r *SQLAccountRepository) patchAccountsInTx(
	ctx context.Context,
	patches []account.AccountPatch,
) (account.AccountMutationResult, error) {
	tx, err := r.beginSQLMutation(ctx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	defer tx.Rollback()
	revision, err := bumpSQLRevision(ctx, tx, r.dialect)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	count, err := patchSQLAccounts(ctx, tx, r.dialect, patches, revision)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{Patched: count, Revision: revision}, nil
}

func patchSQLAccounts(
	ctx context.Context,
	tx localSQLRunner,
	dialect SQLDialect,
	patches []account.AccountPatch,
	revision int,
) (int, error) {
	ts := platformruntime.NowMS()
	count := 0
	for _, patch := range patches {
		affected, err := patchSQLAccount(ctx, tx, dialect, patch, ts, revision)
		if err != nil {
			return 0, err
		}
		count += affected
	}
	return count, nil
}

func patchSQLAccount(
	ctx context.Context,
	tx localSQLRunner,
	dialect SQLDialect,
	patch account.AccountPatch,
	ts int64,
	revision int,
) (int, error) {
	record, found, err := getSQLAccountForPatch(ctx, tx, dialect, patch.Token)
	if err != nil || !found {
		return 0, err
	}
	sets, err := buildSQLPatchSets(record, patch, ts, revision)
	if err != nil {
		return 0, err
	}
	assignments, values := sqlAssignments(dialect, sets)
	values = append(values, patch.Token)
	result, err := tx.ExecContext(ctx, "UPDATE accounts SET "+assignments+
		" WHERE token = "+sqlBind(dialect, len(values)), values...)
	if err != nil {
		return 0, err
	}
	return affectedRows(result)
}

func getSQLAccountForPatch(
	ctx context.Context,
	tx localSQLRunner,
	dialect SQLDialect,
	token string,
) (account.AccountRecord, bool, error) {
	row := tx.QueryRowContext(ctx, "SELECT "+localAccountColumns+" FROM accounts WHERE token = "+sqlBind(dialect, 1), token)
	record, err := scanLocalAccount(row)
	if err == sql.ErrNoRows {
		return account.AccountRecord{}, false, nil
	}
	return record, err == nil, err
}

func buildSQLPatchSets(
	record account.AccountRecord,
	patch account.AccountPatch,
	ts int64,
	revision int,
) ([]localPatchSet, error) {
	sets := []localPatchSet{{"updated_at", ts}, {"revision", revision}}
	sets = appendBasicPatchSets(sets, patch)
	sets = appendUsagePatchSets(sets, record, patch)
	quotaSets, err := quotaPatchSets(patch)
	if err != nil {
		return nil, err
	}
	sets = append(sets, quotaSets...)
	sets = append(sets, localPatchSet{"tags", patchedRedisTags(record.Tags, patch)})
	return appendExtPatchSets(sets, record, patch)
}
