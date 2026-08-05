package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/HONG-LOU/entcoin/entpay"
	"github.com/HONG-LOU/entcoin/internal/core"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "agent":
		err = runAgent(os.Args[2:])
	case "generate-key":
		err = generateKey(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runAgent(arguments []string) error {
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	endpoint := flags.String("endpoint", "", "merchant EntPay HTTPS endpoint")
	data := flags.String("data", "", "Entcoin wallet data directory")
	wallet := flags.String("wallet", "", "dedicated Agent wallet address")
	resource := flags.String("resource", "", "advertised resource ID")
	inputJSON := flags.String("input-json", "", "resource input as a JSON object")
	inputFile := flags.String("input-file", "", "resource input JSON file")
	maximum := flags.String("max-amount", "", "hard payment limit in ENT")
	timeout := flags.Duration("timeout", 5*time.Minute, "confirmation and fulfillment deadline")
	artifactOutput := flags.String("artifact-output", "", "verified artifact destination")
	codex := flags.String("codex", "codex", "Codex executable")
	model := flags.String("model", "", "Codex model override")
	output := flags.String("output", "", "optional JSON result path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if strings.TrimSpace(*endpoint) == "" || strings.TrimSpace(*resource) == "" || strings.TrimSpace(*maximum) == "" || (*inputJSON == "") == (*inputFile == "") {
		return fmt.Errorf("--endpoint, --resource, --max-amount, and exactly one of --input-json or --input-file are required")
	}
	input, err := readInput(*inputJSON, *inputFile)
	if err != nil {
		return err
	}
	maximumAtoms, err := core.ParseAmount(*maximum)
	if err != nil {
		return fmt.Errorf("maximum amount: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := entpay.RunAgent(ctx, entpay.AgentConfig{
		Endpoint: *endpoint, DataDirectory: *data, WalletAddress: *wallet,
		Resource: *resource, Input: input, MaximumAmount: maximumAtoms,
		PaymentTimeout: *timeout, ArtifactOutput: *artifactOutput,
		Reasoner: entpay.CodexReasoner{Executable: *codex, Model: *model},
	})
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if *output != "" {
		if err := os.WriteFile(*output, append(encoded, '\n'), 0o600); err != nil {
			return fmt.Errorf("write agent result: %w", err)
		}
	}
	fmt.Printf("decision:    %s\ntransaction: %s\nreceipt:     %s\n", result.Decision.Reason, result.TransactionID, result.Delivery.Receipt.Signature)
	if result.ArtifactOutput != "" {
		fmt.Println("artifact:    ", result.ArtifactOutput)
	}
	if result.Analysis != "" {
		fmt.Printf("\n%s\n", result.Analysis)
	}
	return nil
}

func readInput(inline, path string) (json.RawMessage, error) {
	contents := []byte(inline)
	if path != "" {
		var err error
		contents, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read input file: %w", err)
		}
	}
	if len(contents) == 0 || len(contents) > 128<<10 || !json.Valid(contents) {
		return nil, fmt.Errorf("resource input must be valid JSON up to 128 KiB")
	}
	return json.RawMessage(contents), nil
}

func generateKey(arguments []string) error {
	flags := flag.NewFlagSet("generate-key", flag.ContinueOnError)
	output := flags.String("output", "", "private signing key path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if strings.TrimSpace(*output) == "" {
		return fmt.Errorf("--output is required")
	}
	if _, err := os.Stat(*output); err == nil {
		return fmt.Errorf("signing key already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o700); err != nil {
		return err
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	encoded := base64.RawURLEncoding.EncodeToString(privateKey) + "\n"
	if err := os.WriteFile(*output, []byte(encoded), 0o600); err != nil {
		return err
	}
	fmt.Println("public signing key:", base64.RawURLEncoding.EncodeToString(publicKey))
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: entpay <agent|generate-key> [options]")
}
