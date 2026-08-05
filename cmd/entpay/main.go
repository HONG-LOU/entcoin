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
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/entpay"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
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

func serve(arguments []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	address := flags.String("listen", envOr("ENTPAY_ADDR", "127.0.0.1:47841"), "HTTP listen address")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	merchant := strings.TrimSpace(os.Getenv("ENTPAY_MERCHANT_ADDRESS"))
	key, err := readSigningKey(os.Getenv("ENTPAY_SIGNING_KEY_FILE"))
	if err != nil {
		return err
	}
	price, err := envUint("ENTPAY_PRICE_ATOMS", 10_000)
	if err != nil {
		return err
	}
	confirmations, err := envUint("ENTPAY_CONFIRMATIONS", 1)
	if err != nil {
		return err
	}
	lifetimeSeconds, err := envUint("ENTPAY_INVOICE_LIFETIME_SECONDS", 900)
	if err != nil {
		return err
	}
	reportNodes := splitNonEmpty(envOr("ENTPAY_REPORT_NODES", "https://node.entcoin.xyz,https://template-chat.xyz"))
	nodeClient, err := entpay.NewNodeClient(envOr("ENTPAY_LOCAL_NODE", "http://127.0.0.1:47821"), reportNodes)
	if err != nil {
		return err
	}
	store, err := entpay.OpenStore(envOr("ENTPAY_DATABASE", "/var/lib/entpay/entpay.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	service, err := entpay.NewService(entpay.Config{
		MerchantAddress: merchant, Price: price, Confirmations: confirmations,
		InvoiceLifetime: time.Duration(lifetimeSeconds) * time.Second,
		SigningKey:      key, NodeClient: nodeClient, Store: store, Logger: logger,
	})
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: *address, Handler: service.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	logger.Info("EntPay listening", "address", *address, "merchant", merchant, "price", price)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runAgent(arguments []string) error {
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	endpoint := flags.String("endpoint", "https://entcoin.xyz/entpay/", "EntPay HTTPS endpoint")
	data := flags.String("data", "", "Entcoin wallet data directory")
	wallet := flags.String("wallet", "", "dedicated Agent wallet address")
	query := flags.String("query", "检查两个 Entcoin 公网节点是否处于同一条主链，并总结当前网络状态。", "paid analysis query")
	maximum := flags.String("max-amount", "0.00010000", "hard payment limit in ENT")
	timeout := flags.Duration("timeout", 3*time.Minute, "confirmation deadline")
	codex := flags.String("codex", "codex", "Codex executable")
	model := flags.String("model", "", "Codex model override")
	output := flags.String("output", "", "optional JSON result path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	maximumAtoms, err := core.ParseAmount(*maximum)
	if err != nil {
		return fmt.Errorf("maximum amount: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := entpay.RunAgent(ctx, entpay.AgentConfig{
		Endpoint: *endpoint, DataDirectory: *data, WalletAddress: *wallet, Query: *query,
		MaximumAmount: maximumAtoms, PaymentTimeout: *timeout,
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
	fmt.Printf("AI decision: %s\ntransaction: %s\nreceipt:     %s\n\n%s\n", result.Decision.Reason, result.Transaction, result.Delivery.Receipt.Signature, result.Analysis)
	return nil
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

func readSigningKey(path string) (ed25519.PrivateKey, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("ENTPAY_SIGNING_KEY_FILE is required")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(contents)))
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("signing key file is invalid")
	}
	return ed25519.PrivateKey(decoded), nil
}

func envUint(name string, fallback uint64) (uint64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an unsigned integer", name)
	}
	return parsed, nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func splitNonEmpty(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: entpay <serve|agent|generate-key> [options]")
}
