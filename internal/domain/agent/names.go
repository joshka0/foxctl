package agent

import "math/rand"

// Agent name generation word lists for memorable random names.
var agentAdjectives = []string{
	"swift", "bright", "clever", "noble", "quiet", "bold", "calm", "keen",
	"wise", "brave", "kind", "fair", "true", "warm", "sharp", "clear",
	"deep", "free", "wild", "soft", "strong", "gentle", "nimble", "steady",
}

var agentNouns = []string{
	"atlas", "nova", "echo", "iris", "luna", "orion", "sage", "phoenix",
	"zephyr", "ember", "cedar", "river", "cliff", "dawn", "frost", "grove",
	"harbor", "jade", "maple", "oak", "peak", "rain", "sky", "tide",
}

// GenerateAgentName generates a memorable hyphenated name composed of a random
// adjective and noun (for example, "swift-atlas").
func GenerateAgentName(rng *rand.Rand) string {
	adj := agentAdjectives[rng.Intn(len(agentAdjectives))]
	noun := agentNouns[rng.Intn(len(agentNouns))]
	return adj + "-" + noun
}
