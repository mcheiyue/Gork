package main

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// 窄切：仅支持 account check；其他子命令仍返回 unknown，避免强依赖 config/healthcheck 整栈。
func runGorkCommand(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) (bool, int, error) {
	if len(args) == 0 {
		return false, 0, nil
	}
	if args[0] != "account" || len(args) < 2 || args[1] != "check" {
		// 非 account check：交给服务模式（main 继续 ListenAndServe）
		if args[0] == "account" {
			return true, 2, fmt.Errorf("unknown command: %s", strings.Join(args, " "))
		}
		return false, 0, nil
	}
	return runAccountCheckCommand(ctx, args[2:], stdout, stderr)
}
