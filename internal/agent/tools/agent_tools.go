package tools

import "fmt"

// registerAgentTools registers agent lifecycle tools.
func (r *Registry) registerAgentTools() error {
	cfg := SpawnToolConfig{
		CallerActorID:       r.config.ActorID,
		CallerDepth:         r.config.Depth,
		CallerMaxDepth:      r.config.MaxDepth,
		CallerLocalMaxDepth: r.config.LocalMaxDepth,
		EpicID:              r.config.EpicID,
		MailSender:          r.spawnMailSender,
	}
	if err := r.RegisterSpawnTool(cfg); err != nil {
		return fmt.Errorf("register agent.spawn: %w", err)
	}
	return nil
}
