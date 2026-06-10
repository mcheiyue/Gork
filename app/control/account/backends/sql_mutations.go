package backends

import (
	"context"
	"database/sql"

	account "github.com/jiujiu532/grok2api/app/control/account"
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
	deleted, err := r.deletePoolForReplace(ctx, command.Pool)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	upserted, err := r.UpsertAccounts(ctx, command.Upserts)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{
		Upserted: upserted.Upserted,
		Deleted:  deleted,
		Revision: upserted.Revision,
	}, nil
}

func (r *SQLAccountRepository) beginSQLMutation(ctx context.Context) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, nil)
}
