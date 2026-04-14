package telegram

import "github.com/jkatigb/agentctl/internal/interfaces/chatadapter"

// MVPCommands returns the MVP set of commands for Telegram.
//
// Telegram commands are names-only (no typed options); this list is used for
// setMyCommands and for parity with Discord.
func MVPCommands() []chatadapter.CommandDef {
	return []chatadapter.CommandDef{
		{
			// Helper for configuring TELEGRAM_* chat IDs.
			Name:        "chat_id",
			Description: "Show this chat's numeric chat_id",
		},
		{
			Name:        "search",
			Description: "Search the codebase by concept",
			Options: []chatadapter.CommandOption{
				{
					Name:        "query",
					Description: "Search query",
					Type:        chatadapter.OptionTypeString,
					Required:    true,
				},
			},
		},
		{
			Name:        "todo",
			Description: "Manage tasks",
			Options: []chatadapter.CommandOption{
				{
					Name:        "action",
					Description: "Action to perform",
					Type:        chatadapter.OptionTypeString,
					Required:    true,
					Choices: []chatadapter.Choice{
						{Name: "list", Value: "list"},
						{Name: "add", Value: "add"},
						{Name: "complete", Value: "complete"},
					},
				},
				{
					Name:        "title",
					Description: "Task title (for add)",
					Type:        chatadapter.OptionTypeString,
				},
				{
					Name:        "id",
					Description: "Task ID (for complete)",
					Type:        chatadapter.OptionTypeString,
				},
			},
		},
		{
			Name:        "memory",
			Description: "Query agent memory",
			Options: []chatadapter.CommandOption{
				{
					Name:        "query",
					Description: "Memory search query",
					Type:        chatadapter.OptionTypeString,
					Required:    true,
				},
			},
		},
		{
			Name:        "logs",
			Description: "View observability logs",
			Options: []chatadapter.CommandOption{
				{
					Name:        "errors_only",
					Description: "Show only errors",
					Type:        chatadapter.OptionTypeBool,
				},
			},
		},
		{
			// Telegram command names must be lowercase letters, digits, and underscores.
			// We'll normalize underscores to hyphens when routing into agentctl.
			Name:        "agent_spawn",
			Description: "Spawn an autonomous agent",
			Options: []chatadapter.CommandOption{
				{
					Name:        "role",
					Description: "Agent role",
					Type:        chatadapter.OptionTypeString,
					Required:    true,
					Choices: []chatadapter.Choice{
						{Name: "researcher", Value: "researcher"},
						{Name: "coder", Value: "coder"},
						{Name: "planner", Value: "planner"},
						{Name: "reviewer", Value: "reviewer"},
						{Name: "overseer", Value: "overseer"},
					},
				},
				{
					Name:        "prompt",
					Description: "Task prompt for the agent",
					Type:        chatadapter.OptionTypeString,
					Required:    true,
				},
			},
		},
		{
			Name:        "agent_list",
			Description: "List running agents",
		},
	}
}
