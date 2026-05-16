// Package main implements the cloud/localstack_blueprint skill.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

const command = "cloud/localstack_blueprint"

type input struct {
	Scenario string `json:"scenario"`
	RunID    string `json:"run_id"`
	Endpoint string `json:"endpoint"`
	Mode     string `json:"mode"`
}

type resource struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type output struct {
	Tool            map[string]any `json:"tool"`
	Scenario        string         `json:"scenario"`
	Mode            string         `json:"mode"`
	RunID           string         `json:"run_id"`
	Endpoint        string         `json:"endpoint"`
	Resources       []resource     `json:"resources"`
	Commands        []string       `json:"commands"`
	ProductionNotes []string       `json:"production_notes"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	if in.Endpoint == "" {
		in.Endpoint = "http://127.0.0.1:4566"
	}
	if strings.ContainsAny(in.Endpoint, " \t\r\n") {
		return skillerr.Arg("endpoint must not contain whitespace")
	}
	if in.Mode == "" {
		in.Mode = "plan"
	}
	if in.Mode != "plan" && in.Mode != "apply" {
		return skillerr.Arg("mode must be plan or apply")
	}
	if in.RunID == "" {
		in.RunID = shortID()
	}

	resources, commands, err := blueprint(in.Scenario, in.RunID, in.Endpoint)
	if err != nil {
		return err
	}

	if in.Mode == "apply" {
		for i, cmd := range commands {
			if i < 2 {
				continue
			}
			if err := runAWS(ctx, cmd); err != nil {
				return skillerr.WrapRuntime(fmt.Sprintf("apply %s", cmd), err)
			}
		}
		for i := range resources {
			resources[i].Status = "created"
		}
	}

	out := output{
		Tool: map[string]any{
			"name":        command,
			"description": "Plan or apply a LocalStack-only AWS blueprint and return the changed resources.",
			"arguments": map[string]any{
				"scenario": in.Scenario,
				"run_id":   in.RunID,
				"endpoint": in.Endpoint,
				"mode":     in.Mode,
			},
		},
		Scenario:  in.Scenario,
		Mode:      in.Mode,
		RunID:     in.RunID,
		Endpoint:  in.Endpoint,
		Resources: resources,
		Commands:  commands,
		ProductionNotes: []string{
			"Keep LocalStack and real AWS credentials separated.",
			"Require OIDC role assumption, scoped IAM, cost checks, and drift checks before production.",
			"Emit audit events for plan, apply, and teardown.",
			"Attach Grafana or runbook links to every applied blueprint.",
		},
	}

	return skillout.Emit(rc, command, out)
}

func blueprint(scenario, runID, endpoint string) ([]resource, []string, error) {
	bucket := safeName("portfolio-" + scenario + "-" + runID)
	queue := safeName("portfolio-" + scenario + "-events-" + runID)
	commands := []string{
		"export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1",
		fmt.Sprintf("aws --endpoint-url=%s s3 ls", endpoint),
	}

	switch scenario {
	case "event-pipeline":
		commands = append(
			commands,
			fmt.Sprintf("aws --endpoint-url=%s s3api create-bucket --bucket %s", endpoint, bucket),
			fmt.Sprintf("aws --endpoint-url=%s sqs create-queue --queue-name %s", endpoint, queue),
		)
		return []resource{
			{Type: "s3_bucket", Name: bucket, Status: "planned"},
			{Type: "sqs_queue", Name: queue, Status: "planned"},
		}, commands, nil
	case "gitops-state", "eks-observability":
		commands = append(commands, fmt.Sprintf("aws --endpoint-url=%s s3api create-bucket --bucket %s", endpoint, bucket))
		return []resource{{Type: "s3_bucket", Name: bucket, Status: "planned"}}, commands, nil
	default:
		return nil, nil, skillerr.Arg("unknown scenario", skillerr.WithHint("Use event-pipeline, gitops-state, or eks-observability."))
	}
}

func runAWS(ctx context.Context, commandLine string) error {
	parts := strings.Fields(commandLine)
	if len(parts) == 0 || parts[0] != "aws" {
		return nil
	}
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Env = append(
		os.Environ(),
		"AWS_ACCESS_KEY_ID=test",
		"AWS_SECRET_ACCESS_KEY=test",
		"AWS_DEFAULT_REGION=us-east-1",
	)
	return cmd.Run()
}

func safeName(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := b.String()
	if len(out) > 63 {
		return out[:63]
	}
	return out
}

func shortID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "local"
	}
	return hex.EncodeToString(b[:])
}
