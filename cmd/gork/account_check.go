package main

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// runGorkCommand 分流 CLI 子命令；未识别的非服务参数返回 unknown。
// 服务模式：空 args 或未注册子命令前缀返回 handled=false，由 main 继续 ListenAndServe。
func runGorkCommand(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) (bool, int, error) {
	if len(args) == 0 {
		return false, 0, nil
	}
	switch args[0] {
	case "healthcheck":
		return runHealthcheckCommand(ctx, args[1:], stdout, stderr)
	case "account":
		if len(args) < 2 || args[1] != "check" {
			return true, 2, fmt.Errorf("unknown command: %s", strings.Join(args, " "))
		}
		return runAccountCheckCommand(ctx, args[2:], stdout, stderr)
	default:
		// 非 CLI 子命令：交给服务模式（兼容历史 serve / 直接起服）
		// 仅当明确是已知 CLI 家族以外的词时，仍走服务模式，避免误伤。
		// 上游对未知命令返回 error；本树保持「未知词 = 起服」以免破坏无参部署习惯。
		// 对明确的 CLI 错拼（如 account xxx）已在上面处理。
		return false, 0, nil
	}
}
