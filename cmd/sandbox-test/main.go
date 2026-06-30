package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/execution/k8ssandbox"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Direct mode with pod IP from env or arg
	podIP := os.Getenv("SANDBOX_POD_IP")
	if podIP == "" {
		podIP = "10.42.0.116"
	}
	apiURL := fmt.Sprintf("http://%s:8888", podIP)
	fmt.Printf("Connecting to: %s\n", apiURL)

	runner, err := k8ssandbox.NewRunner(ctx, k8ssandbox.Config{
		WarmPool:       "foxctl-test-pool",
		Namespace:      "agent-sandbox-demo",
		Mode:           "direct",
		APIURL:         apiURL,
		CommandTimeout: 30 * time.Second,
	})
	if err != nil {
		log.Fatalf("NewRunner: %v", err)
	}
	defer runner.Close(ctx)

	// Test echo
	result, err := runner.ExecuteRaw(ctx, "echo 'Hello via k8ssandbox runner!'", nil, nil)
	if err != nil {
		log.Fatalf("ExecuteRaw: %v", err)
	}
	fmt.Printf("ExitCode: %d\nStdout: %s\n", result.ExitCode, result.Stdout)

	// Test python
	result2, err := runner.ExecuteRaw(ctx, "python3 -c \"print(2+2)\"", nil, nil)
	if err != nil {
		log.Fatalf("ExecuteRaw(python): %v", err)
	}
	fmt.Printf("Python 2+2: %s\n", result2.Stdout)

	// Test env vars
	result3, err := runner.ExecuteRaw(ctx, "echo $MY_VAR", nil, []string{"MY_VAR=hello_from_foxctl"})
	if err != nil {
		log.Fatalf("ExecuteRaw(env): %v", err)
	}
	fmt.Printf("Env var: %s\n", result3.Stdout)

	// Test file I/O via stdin
	result4, err := runner.ExecuteRaw(ctx, "cat input.json", []byte(`{"test":"data"}`), nil)
	if err != nil {
		log.Fatalf("ExecuteRaw(file): %v", err)
	}
	fmt.Printf("File read: %s\n", result4.Stdout)

	fmt.Println("\nAll tests passed!")
}
