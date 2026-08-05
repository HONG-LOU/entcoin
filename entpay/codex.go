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

func (r CodexReasoner) Approve(ctx context.Context, request ApprovalRequest) (Decision, error) {
	payload, err := json.Marshal(struct {
		ApprovalRequest
		EvaluatedAt   time.Time `json:"evaluated_at"`
		PolicyChecked bool      `json:"deterministic_policy_checked"`
	}{ApprovalRequest: request, EvaluatedAt: time.Now().UTC(), PolicyChecked: true})
	if err != nil {
		return Decision{}, err
	}
	schema := `{"type":"object","additionalProperties":false,"required":["approved","reason"],"properties":{"approved":{"type":"boolean"},"reason":{"type":"string"}}}`
	prompt := "You are a payment approval agent operating under a strict spending limit. Deterministic code has already verified HTTPS, protocol, network, merchant address, Ed25519 signature, input hash, advertised price, capabilities, expiry, and the hard amount limit. Do not second-guess those verified facts. Decide only whether the supplied user input reasonably matches the advertised product and whether buying it at the displayed price is sensible. Reject ambiguous, abusive, or unrelated requests. Return a concise reason in the user's language. Do not run commands.\n\n" + string(payload)
	output, err := r.run(ctx, prompt, schema)
	if err != nil {
		return Decision{}, err
	}
	var decision Decision
	if err := json.Unmarshal([]byte(output), &decision); err != nil {
		return Decision{}, fmt.Errorf("decode Codex decision: %w", err)
	}
	if strings.TrimSpace(decision.Reason) == "" {
		return Decision{}, fmt.Errorf("Codex decision omitted its reason")
	}
	return decision, nil
}

func (r CodexReasoner) Analyze(ctx context.Context, delivery Delivery) (string, error) {
	payload, err := json.Marshal(delivery)
	if err != nil {
		return "", err
	}
	prompt := "You are analyzing a paid merchant delivery that deterministic code has already verified against its signed receipt, payload hash, and optional artifact hash. Summarize what was delivered, the practical result, and any caveat visible in the payload. Use the user's language when it is apparent from the content. Do not claim facts outside the delivery and do not run commands.\n\n" + string(payload)
	return r.run(ctx, prompt, "")
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
