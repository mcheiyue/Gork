package backends

import (
	"context"
	"database/sql"

	account "github.com/jiujiu532/grok2api/app/control/account"
)

type localPatchSet struct {
	column string
	value  any
}

func (r *LocalAccountRepository) UpsertAccounts(
	ctx context.Context,
	items []account.AccountUpsert,
) (account.AccountMutationResult, error) {
	if len(items) == 0 {
		return account.AccountMutationResult{}, nil
	}
	r.lock.Lock()
	defer r.lock.Unlock()
	db, tx, err := r.beginMutation(ctx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	defer db.Close()
	defer tx.Rollback()
	revision, err := bumpLocalRevision(ctx, tx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	count, err := upsertLocalAccounts(ctx, tx, items, revision)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{Upserted: count, Revision: revision}, nil
}

func (r *LocalAccountRepository) PatchAccounts(
	ctx context.Context,
	patches []account.AccountPatch,
) (account.AccountMutationResult, error) {
	if len(patches) == 0 {
		return account.AccountMutationResult{}, nil
	}
	r.lock.Lock()
	defer r.lock.Unlock()
	db, tx, err := r.beginMutation(ctx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	defer db.Close()
	defer tx.Rollback()
	revision, err := bumpLocalRevision(ctx, tx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	count, err := patchLocalAccounts(ctx, tx, patches, revision)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{Patched: count, Revision: revision}, nil
}

func (r *LocalAccountRepository) DeleteAccounts(
	ctx context.Context,
	tokens []string,
) (account.AccountMutationResult, error) {
	if len(tokens) == 0 {
		return account.AccountMutationResult{}, nil
	}
	r.lock.Lock()
	defer r.lock.Unlock()
	db, tx, err := r.beginMutation(ctx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	defer db.Close()
	defer tx.Rollback()
	revision, err := bumpLocalRevision(ctx, tx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	count, err := deleteLocalAccounts(ctx, tx, tokens, revision)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{Deleted: count, Revision: revision}, nil
}

func (r *LocalAccountRepository) ReplacePool(
	ctx context.Context,
	command account.BulkReplacePoolCommand,
) (account.AccountMutationResult, error) {
	command.Normalize()
	r.lock.Lock()
	defer r.lock.Unlock()
	db, tx, err := r.beginMutation(ctx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	defer db.Close()
	defer tx.Rollback()
	deletedRev, err := bumpLocalRevision(ctx, tx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	deleted, err := deleteLocalPool(ctx, tx, command.Pool, deletedRev)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	upsertRev, err := bumpLocalRevision(ctx, tx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	upserted, err := upsertLocalAccounts(ctx, tx, command.Upserts, upsertRev)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{Upserted: upserted, Deleted: deleted, Revision: upsertRev}, nil
}

func (r *LocalAccountRepository) beginMutation(ctx context.Context) (*sql.DB, *sql.Tx, error) {
	db, err := r.connect(ctx)
	if err != nil {
		return nil, nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return db, tx, nil
}
