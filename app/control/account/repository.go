package account

import "context"

const AccountScanChangesDefaultLimit = 5000

type AccountRepository interface {
	Initialize(context.Context) error
	GetRevision(context.Context) (int, error)
	RuntimeSnapshot(context.Context) (RuntimeSnapshot, error)
	ScanChanges(context.Context, int, int) (AccountChangeSet, error)
	UpsertAccounts(context.Context, []AccountUpsert) (AccountMutationResult, error)
	PatchAccounts(context.Context, []AccountPatch) (AccountMutationResult, error)
	DeleteAccounts(context.Context, []string) (AccountMutationResult, error)
	GetAccounts(context.Context, []string) ([]AccountRecord, error)
	ListAccounts(context.Context, ListAccountsQuery) (AccountPage, error)
	ReplacePool(context.Context, BulkReplacePoolCommand) (AccountMutationResult, error)
	Close(context.Context) error
}
