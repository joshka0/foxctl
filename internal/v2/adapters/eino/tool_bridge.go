package eino

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

// einoToolBridge implements Eino's tool.InvokableTool by wrapping an foxctl
// engine.ToolDef and engine.ToolExecutor.
type einoToolBridge struct {
	def      engine.ToolDef
	executor engine.ToolExecutor
}

// NewEinoToolBridge creates a new Eino tool from an foxctl ToolDef and ToolExecutor.
func NewEinoToolBridge(def engine.ToolDef, executor engine.ToolExecutor) tool.InvokableTool {
	return &einoToolBridge{
		def:      def,
		executor: executor,
	}
}

// NewEinoToolBridges creates a slice of Eino tools from foxctl ToolDefs and a single ToolExecutor.
func NewEinoToolBridges(defs []engine.ToolDef, executor engine.ToolExecutor) []tool.InvokableTool {
	if len(defs) == 0 {
		return nil
	}
	out := make([]tool.InvokableTool, 0, len(defs))
	for _, def := range defs {
		out = append(out, NewEinoToolBridge(def, executor))
	}
	return out
}

// Info implements tool.BaseTool by mapping foxctl ToolDef to Eino schema.ToolInfo.
func (b *einoToolBridge) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info := &schema.ToolInfo{
		Name: b.def.Name,
		Desc: b.def.Description,
	}

	if len(b.def.Parameters) > 0 {
		var js jsonschema.Schema
		if err := json.Unmarshal(b.def.Parameters, &js); err != nil {
			return nil, fmt.Errorf("eino tool bridge: unmarshal parameters for %q: %w", b.def.Name, err)
		}
		info.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(&js)
	}

	return info, nil
}

// InvokableRun implements tool.InvokableTool by delegating to the foxctl ToolExecutor.
func (b *einoToolBridge) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	if b.executor == nil {
		return "", fmt.Errorf("eino tool bridge: nil executor for tool %q", b.def.Name)
	}

	// argumentsInJSON is expected to be a raw JSON string from the model.
	// foxctl ToolExecutor.Execute expects json.RawMessage.
	return b.executor.Execute(ctx, b.def.Name, json.RawMessage(argumentsInJSON))
}
