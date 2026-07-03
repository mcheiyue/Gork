package backends

import (
	"context"
	"database/sql"

	account "github.com/dslzl/gork/app/control/account"
)

func (r *SQLAccountRepository) UpsertAccounts(
	ctx context.Context,
	items []account.AccountUpsert,
) (account.AccountMutationResult, error) {
	if len(items) == 0 {
		return account.AccountMutationResult{}, nil
	}
	if err := r.ensureInitialized(ctx); err != nil {
		return account.AccountMutationResult{}, err
	}
	r.mutationMux.Lock()
	defer r.mutationMux.Unlock()
	return r.upsertAccountsInTx(ctx, items)
}

func (r *SQLAccountRepository) PatchAccounts(
	ctx context.Context,
	patches []account.AccountPatch,
) (account.AccountMutationResult, error) {
	if len(patches) == 0 {
		return account.AccountMutationResult{}, nil
	}
	if err := r.ensureInitialized(ctx); err != nil {
		return account.AccountMutationResult{}, err
	}
	r.mutationMux.Lock()
	defer r.mutationMux.Unlock()
	return r.patchAccountsInTx(ctx, patches)
}

func (r *SQLAccountRepository) DeleteAccounts(
	ctx context.Context,
	tokens []string,
) (account.AccountMutationResult, error) {
	if len(tokens) == 0 {
		return account.AccountMutationResult{}, nil
	}
	if err := r.ensureInitialized(ctx); err != nil {
		return account.AccountMutationResult{}, err
	}
	r.mutationMux.Lock()
	defer r.mutationMux.Unlock()
	return r.deleteAccountsInTx(ctx, tokens)
}

func (r *SQLAccountRepository) ReplacePool(
	ctx context.Context,
	command account.BulkReplacePoolCommand,
) (account.AccountMutationResult, error) {
	command.Normalize()
	if err := r.ensureInitialized(ctx); err != nil {
		return account.AccountMutationResult{}, err
	}
	r.mutationMux.Lock()
	defer r.mutationMux.Unlock()
	tx, err := r.beginSQLMutation(ctx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	defer tx.Rollback()
	deletedRev, err := bumpSQLRevision(ctx, tx, r.dialect)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	deleted, err := deleteSQLPool(ctx, tx, r.dialect, command.Pool, deletedRev)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	upsertRev, err := bumpSQLRevision(ctx, tx, r.dialect)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	upserted, err := upsertSQLAccounts(ctx, tx, r.dialect, command.Upserts, upsertRev)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{
		Upserted: upserted,
		Deleted:  deleted,
		Revision: upsertRev,
	}, nil
}

func (r *SQLAccountRepository) beginSQLMutation(ctx context.Context) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, nil)
}
