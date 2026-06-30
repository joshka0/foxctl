package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/execution"
	"github.com/joshka0/foxctl/internal/runtime/execution/k8ssandbox"
	sandbox "sigs.k8s.io/agent-sandbox/clients/go/sandbox"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Direct mode: connect straight to the sandbox runtime HTTP server.
	// Bypasses sandbox-router-svc discovery which requires a stable service selector.
	opts := sandbox.Options{
		WarmPoolName: "foxctl-test-pool",
		APIURL:       "http://10.42.0.13:8888",
	}

	client, err := sandbox.NewClient(ctx, opts)
	if err != nil {
		log.Fatalf("NewClient: %v", err)
	}

	sb, err := client.CreateSandbox(ctx, "foxctl-test-pool", "agent-sandbox-demo")
	if err != nil {
		log.Fatalf("CreateSandbox: %v", err)
	}
	defer sb.Close(ctx)

	// Test 1: Simple echo command
	result, err := sb.Run(ctx, "echo 'Hello from agent-sandbox!'")
	if err != nil {
		log.Fatalf("Run(echo): %v", err)
	}
	fmt.Printf("Test 1 - echo:\n  ExitCode: %d\n  Stdout: %s\n", result.ExitCode, result.Stdout)

	// Test 2: Write a file then read it back
	err = sb.Write(ctx, "test.txt", []byte("sandbox file content"))
	if err != nil {
		log.Fatalf("Write: %v", err)
	}
	data, err := sb.Read(ctx, "test.txt")
	if err != nil {
		log.Fatalf("Read: %v", err)
	}
	fmt.Printf("Test 2 - file I/O:\n  Written: 'sandbox file content'\n  Read back: '%s'\n", string(data))

	// Test 3: Run a Python one-liner (python-runtime-sandbox image has python3)
	result, err = sb.Run(ctx, "python3 -c \"print(2+2)\"")
	if err != nil {
		log.Fatalf("Run(python): %v", err)
	}
	fmt.Printf("Test 3 - python3:\n  ExitCode: %d\n  Stdout: %s\n", result.ExitCode, result.Stdout)

	// Test 4: Execute via the k8ssandbox Runner (tests our SkillExecutor impl)
	fmt.Println("\n--- Testing k8ssandbox.Runner ---")
	runner, err := k8ssandbox.NewRunner(ctx, k8ssandbox.Config{
		WarmPool:       "foxctl-test-pool",
		Namespace:      "agent-sandbox-demo",
		Mode:           "direct",
		APIURL:         "http://10.42.0.13:8888",
		CommandTimeout: 30 * time.Second,
	})
	if err != nil {
		log.Fatalf("NewRunner: %v", err)
	}
	defer runner.Close(ctx)

	execResult, err := runner.Execute(ctx, execution.ExecuteOptions{
		ArtifactPath: "echo 'Skill execution via k8ssandbox runner!'",
	})
	if err != nil {
		log.Fatalf("Runner.Execute: %v", err)
	}
	fmt.Printf("Test 4 - Runner:\n  ExitCode: %d\n  Stdout: %s\n", execResult.ExitCode, execResult.Stdout)

	fmt.Println("\nAll tests passed!")
}
