package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/execution/k8ssandbox"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	runner, err := k8ssandbox.NewRunner(ctx, k8ssandbox.Config{
		WarmPool:       os.Getenv("FOXCTL_K8S_SANDBOX_WARMPOOL"),
		Namespace:      "agent-sandbox-demo",
		Mode:           "direct",
		APIURL:         os.Getenv("FOXCTL_K8S_SANDBOX_API_URL"),
		CommandTimeout: 10 * time.Second,
	})
	if err != nil {
		fmt.Printf("NewRunner error: %v\n", err)
		return
	}
	defer runner.Close(ctx)

	// Test with env vars (simulating what the runner switch does)
	env := []string{"FOO=bar", "BAZ=qux"}
	result, err := runner.ExecuteRaw(ctx, "echo hello", []byte(`{}`), env)
	if err != nil {
		fmt.Printf("ExecuteRaw error: %v\n", err)
		return
	}
	fmt.Printf("Result: exit=%d stdout=%q stderr=%q\n", result.ExitCode, result.Stdout, result.Stderr)
}
