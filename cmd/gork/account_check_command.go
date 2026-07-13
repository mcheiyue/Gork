package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	accountbackends "github.com/dslzl/gork/app/control/account/backends"
)

func runAccountCheckCommand(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) (bool, int, error) {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		default:
			return true, 2, fmt.Errorf("unknown account check flag: %s", arg)
		}
	}
	report, err := runAccountCheck(ctx, accountbackends.RepositoryConstructors{})
	if err != nil {
		return true, 1, err
	}
	if jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return true, 1, err
		}
	} else {
		fmt.Fprintf(stdout, "ok=%t revision=%d snapshot=%d list=%d issues=%d\n", report.OK, report.Revision, report.SnapshotCount, report.ListCount, len(report.Issues))
		for _, issue := range report.Issues {
			fmt.Fprintf(stdout, "%s %s %s\n", issue.Code, issue.Token, issue.Message)
		}
	}
	_ = stderr
	if !report.OK {
		return true, 1, nil
	}
	return true, 0, nil
}

func runAccountCheck(ctx context.Context, constructors accountbackends.RepositoryConstructors) (accountcontrol.AccountConsistencyReport, error) {
	repo, err := accountbackends.CreateRepository(commandEnv(), constructors)
	if err != nil {
		return accountcontrol.AccountConsistencyReport{}, err
	}
	if err := repo.Initialize(ctx); err != nil {
		_ = repo.Close(ctx)
		return accountcontrol.AccountConsistencyReport{}, err
	}
	defer func() { _ = repo.Close(ctx) }()
	return accountcontrol.CheckAccountRepositoryConsistency(ctx, repo)
}

func commandEnv() map[string]string {
	env := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}
