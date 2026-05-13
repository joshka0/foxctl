package actor

import (
	"context"
	"testing"
)

func BenchmarkBaseActorLifecycle(b *testing.B) {
	ctx := context.Background()
	actor := NewBaseActor(DefaultConfig("bench-actor"))

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := actor.Start(ctx); err != nil {
			b.Fatalf("Start() error = %v", err)
		}
		if actor.State() != StateIdle {
			b.Fatalf("state after Start() = %s, want %s", actor.State(), StateIdle)
		}
		if err := actor.Stop(ctx); err != nil {
			b.Fatalf("Stop() error = %v", err)
		}
		if actor.State() != StateStopped {
			b.Fatalf("state after Stop() = %s, want %s", actor.State(), StateStopped)
		}
	}
}

func BenchmarkBaseActorMessageDispatch(b *testing.B) {
	ctx := context.Background()
	actor := NewBaseActor(DefaultConfig("bench-actor"))
	actor.RegisterHandler("bench.ask", func(_ context.Context, msg *Message) (*Message, error) {
		if msg.Subject == "" {
			b.Fatal("message subject is empty")
		}
		return nil, nil
	})
	msg := &Message{
		ID:      "bench-message",
		Subject: "bench.ask",
		Body:    []byte("payload"),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := actor.OnMailReceived(ctx, msg); err != nil {
			b.Fatalf("OnMailReceived() error = %v", err)
		}
	}
}
