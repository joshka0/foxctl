package audit_test

import (
	"context"
	"fmt"

	"github.com/joshka0/foxcular"
	"github.com/joshka0/foxcular/audit"
)

// exampleDrain collects events for inspection in examples.
type exampleDrain struct {
	events []*foxcular.Event
}

func (d *exampleDrain) Send(_ context.Context, event *foxcular.Event) error {
	d.events = append(d.events, event)
	return nil
}

func (d *exampleDrain) Flush(_ context.Context) error { return nil }
func (d *exampleDrain) Close() error                  { return nil }

// Example_auditChain demonstrates creating and verifying an audit chain.
func Example_auditChain() {
	key := []byte("audit-secret-key-32-bytes-long!!")
	sink := &exampleDrain{}
	auditDrain := audit.NewAuditDrain(key, sink)

	client := foxcular.NewClient(auditDrain, foxcular.WithSampler(foxcular.AlwaysSample{}))

	_ = client.Emit("user.action").
		WithData("action", "login").
		Success(context.Background(), 0)
	_ = client.Emit("user.action").
		WithData("action", "update_profile").
		Success(context.Background(), 0)
	_ = client.Emit("user.action").
		WithData("action", "logout").
		Success(context.Background(), 0)

	_ = client.Flush(context.Background())
	_ = client.Close()

	// Verify the chain integrity.
	err := audit.Verify(key, sink.events)
	fmt.Printf("verified=%v\n", err == nil)
	// Output:
	// verified=true
}

// Example_auditTamperDetection demonstrates tamper detection.
func Example_auditTamperDetection() {
	key := []byte("audit-secret-key-32-bytes-long!!")
	sink := &exampleDrain{}
	auditDrain := audit.NewAuditDrain(key, sink)

	client := foxcular.NewClient(auditDrain, foxcular.WithSampler(foxcular.AlwaysSample{}))

	_ = client.Emit("document.save").
		WithData("doc_id", "doc-1").
		Success(context.Background(), 0)
	_ = client.Flush(context.Background())

	// Tamper with the event data.
	sink.events[0].Data["doc_id"] = "doc-TAMPERED"

	err := audit.Verify(key, sink.events)
	fmt.Printf("tamper_detected=%v\n", err != nil)
	// Output:
	// tamper_detected=true
}

// Example_auditWrongKey demonstrates that wrong keys fail verification.
func Example_auditWrongKey() {
	key := []byte("audit-secret-key-32-bytes-long!!")
	wrongKey := []byte("wrong-key-32-bytes-long!!!!!!!!!")
	sink := &exampleDrain{}
	auditDrain := audit.NewAuditDrain(key, sink)

	client := foxcular.NewClient(auditDrain, foxcular.WithSampler(foxcular.AlwaysSample{}))

	_ = client.Emit("config.change").
		WithData("key", "max_retries").
		Success(context.Background(), 0)
	_ = client.Flush(context.Background())

	err := audit.VerifyWithKey(wrongKey, sink.events)
	fmt.Printf("wrong_key_fails=%v\n", err != nil)
	// Output:
	// wrong_key_fails=true
}
