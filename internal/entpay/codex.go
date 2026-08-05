package entpay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type CodexReasoner struct {
	Executable string
	Model      string
}

func (r CodexReasoner) Approve(ctx context.Context, invoice Invoice, info Info, query string, maximum uint64) (Decision, error) {
	payload, err := json.Marshal(struct {
		Invoice       Invoice   `json:"invoice"`
		Info          Info      `json:"service"`
		Query         string    `json:"query"`
		Maximum       uint64    `json:"maximum_amount"`
		EvaluatedAt   time.Time `json:"evaluated_at"`
		PolicyChecked bool      `json:"deterministic_policy_checked"`
	}{invoice, info, query, maximum, time.Now().UTC(), true})
	if err != nil {
		return Decision{}, err
	}
	schema := `{"type":"object","additionalProperties":false,"required":["approved","reason"],"properties":{"approved":{"type":"boolean"},"reason":{"type":"string"}}}`
	output, err := r.run(ctx, "你是一个受严格额度约束的支付 Agent。客户端已经用确定性代码验证了 HTTPS 端点、Ed25519 签名、协议、网络、收款地址、金额硬上限和发票有效期；这些已验证事实不需要也不允许你重复猜测。你只判断用户查询是否适合购买该资源，以及已展示的金额是否值得批准。若用户意图、资源和金额一致则批准；不要运行命令。\n\n"+string(payload), schema)
	if err != nil {
		return Decision{}, err
	}
	var decision Decision
	if err := json.Unmarshal([]byte(output), &decision); err != nil {
		return Decision{}, fmt.Errorf("decode Codex decision: %w", err)
	}
	return decision, nil
}

func (r CodexReasoner) Analyze(ctx context.Context, delivery Delivery) (string, error) {
	payload, err := json.Marshal(delivery)
	if err != nil {
		return "", err
	}
	return r.run(ctx, "你是 Entcoin 网络分析 Agent。以下是已通过链上支付解锁并附带 receipt 的实时节点报告。用简洁中文回答报告中的 query，明确两个节点是否一致，列出高度、tip 缩写和任何异常。不得声称报告之外的事实，也不要运行命令。\n\n"+string(payload), "")
}

func (r CodexReasoner) run(ctx context.Context, prompt, schema string) (string, error) {
	executable := strings.TrimSpace(r.Executable)
	if executable == "" {
		executable = "codex"
	}
	directory, err := os.MkdirTemp("", "entpay-codex-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	outputPath := filepath.Join(directory, "output.txt")
	arguments := []string{"exec", "--ephemeral", "--sandbox", "read-only", "--skip-git-repo-check", "--color", "never", "-C", directory, "-o", outputPath}
	if strings.TrimSpace(r.Model) != "" {
		arguments = append(arguments, "--model", r.Model)
	}
	if schema != "" {
		schemaPath := filepath.Join(directory, "schema.json")
		if err := os.WriteFile(schemaPath, []byte(schema), 0o600); err != nil {
			return "", err
		}
		arguments = append(arguments, "--output-schema", schemaPath)
	}
	arguments = append(arguments, "-")
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Stdin = strings.NewReader(prompt)
	var diagnostic bytes.Buffer
	command.Stdout = &diagnostic
	command.Stderr = &diagnostic
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("Codex failed: %w: %s", err, strings.TrimSpace(diagnostic.String()))
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("read Codex output: %w", err)
	}
	return strings.TrimSpace(string(contents)), nil
}
