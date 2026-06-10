package backends

import (
	"context"
	"errors"
	"testing"

	account "github.com/jiujiu532/grok2api/app/control/account"
)

type fakeRepository struct{}

func (fakeRepository) Initialize(context.Context) error         { return nil }
func (fakeRepository) GetRevision(context.Context) (int, error) { return 0, nil }
func (fakeRepository) RuntimeSnapshot(context.Context) (account.RuntimeSnapshot, error) {
	return account.NewRuntimeSnapshot(), nil
}
func (fakeRepository) ScanChanges(context.Context, int, int) (account.AccountChangeSet, error) {
	return account.NewAccountChangeSet(), nil
}
func (fakeRepository) UpsertAccounts(context.Context, []account.AccountUpsert) (account.AccountMutationResult, error) {
	return account.AccountMutationResult{}, nil
}
func (fakeRepository) PatchAccounts(context.Context, []account.AccountPatch) (account.AccountMutationResult, error) {
	return account.AccountMutationResult{}, nil
}
func (fakeRepository) DeleteAccounts(context.Context, []string) (account.AccountMutationResult, error) {
	return account.AccountMutationResult{}, nil
}
func (fakeRepository) GetAccounts(context.Context, []string) ([]account.AccountRecord, error) {
	return nil, nil
}
func (fakeRepository) ListAccounts(context.Context, account.ListAccountsQuery) (account.AccountPage, error) {
	return account.NewAccountPage(), nil
}
func (fakeRepository) ReplacePool(context.Context, account.BulkReplacePoolCommand) (account.AccountMutationResult, error) {
	return account.AccountMutationResult{}, nil
}
func (fakeRepository) Close(context.Context) error { return nil }

func TestGetRepositoryBackend(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    string
		wantErr string
	}{
		{name: "default", env: map[string]string{}, want: "local"},
		{name: "empty", env: map[string]string{"ACCOUNT_STORAGE": ""}, want: "local"},
		{name: "case normalized", env: map[string]string{"ACCOUNT_STORAGE": " REDIS "}, want: "redis"},
		{
			name:    "bad",
			env:     map[string]string{"ACCOUNT_STORAGE": "bad"},
			wantErr: "Unknown account storage backend: 'bad'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetRepositoryBackend(tt.env)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("GetRepositoryBackend = %q/%v, want %q/nil", got, err, tt.want)
			}
		})
	}
}

func TestDescribeRepositoryTargetAndRedaction(t *testing.T) {
	backend, target, err := DescribeRepositoryTarget(map[string]string{
		"ACCOUNT_STORAGE":   "redis",
		"ACCOUNT_REDIS_URL": "redis://user:pass@example.com:6379/0?x=1#f",
	})
	if err != nil || backend != "redis" || target != "redis://user:***@example.com:6379/0?x=1#f" {
		t.Fatalf("redis target = %q/%q/%v", backend, target, err)
	}
	backend, target, err = DescribeRepositoryTarget(map[string]string{
		"ACCOUNT_STORAGE":   "mysql",
		"ACCOUNT_MYSQL_URL": "mysql://user@example.com/db",
	})
	if err != nil || backend != "mysql" || target != "mysql://user:***@example.com/db" {
		t.Fatalf("mysql target = %q/%q/%v", backend, target, err)
	}
	if got := RedactRepositoryURL(""); got != "<empty>" {
		t.Fatalf("empty redaction = %q", got)
	}
	if got := RedactRepositoryURL("not-a-url"); got != "not-a-url" {
		t.Fatalf("plain redaction = %q", got)
	}
}

func TestCreateRepositoryDispatchesConfiguredBackend(t *testing.T) {
	calls := []string{}
	constructors := RepositoryConstructors{
		Local: func(string) (account.AccountRepository, error) {
			calls = append(calls, "local")
			return fakeRepository{}, nil
		},
		Redis: func(string) (account.AccountRepository, error) {
			calls = append(calls, "redis")
			return fakeRepository{}, nil
		},
		MySQL: func(string) (account.AccountRepository, error) {
			calls = append(calls, "mysql")
			return fakeRepository{}, nil
		},
		PostgreSQL: func(string) (account.AccountRepository, error) {
			calls = append(calls, "postgresql")
			return fakeRepository{}, nil
		},
	}
	_, err := CreateRepository(map[string]string{"ACCOUNT_STORAGE": "postgresql", "ACCOUNT_POSTGRESQL_URL": "postgres://db"}, constructors)
	if err != nil {
		t.Fatalf("CreateRepository returned error: %v", err)
	}
	if len(calls) != 1 || calls[0] != "postgresql" {
		t.Fatalf("constructor calls = %#v", calls)
	}
	_, err = CreateRepository(map[string]string{"ACCOUNT_STORAGE": "redis"}, constructors)
	if err == nil {
		t.Fatal("missing redis URL should error")
	}
	_, err = CreateRepository(map[string]string{"ACCOUNT_STORAGE": "local"}, RepositoryConstructors{
		Local: func(string) (account.AccountRepository, error) { return nil, errors.New("boom") },
	})
	if err == nil {
		t.Fatal("constructor error should propagate")
	}
}

func TestCreateRepositoryBuildsDefaultRedisRepository(t *testing.T) {
	repo, err := CreateRepository(map[string]string{
		"ACCOUNT_STORAGE":   "redis",
		"ACCOUNT_REDIS_URL": "redis://localhost:6379/0",
	}, RepositoryConstructors{})
	if err != nil {
		t.Fatalf("CreateRepository redis returned error: %v", err)
	}
	if _, ok := repo.(*RedisAccountRepository); !ok {
		t.Fatalf("CreateRepository redis = %T, want *RedisAccountRepository", repo)
	}
	if err := repo.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}
