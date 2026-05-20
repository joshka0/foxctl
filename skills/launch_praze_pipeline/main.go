package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

const command = "launch/praze_pipeline"

type input struct {
	RunID               string `json:"run_id,omitempty"`
	OutputDir           string `json:"output_dir,omitempty"`
	Write               bool   `json:"write,omitempty"`
	ResearchMode        string `json:"research_mode,omitempty"`
	Limit               int    `json:"limit,omitempty"`
	Prototype           bool   `json:"prototype,omitempty"`
	MockData            bool   `json:"mock_data,omitempty"`
	IncludeFileContents bool   `json:"include_file_contents,omitempty"`
	AgentProvider       string `json:"agent_provider,omitempty"`
	Provision           bool   `json:"provision,omitempty"`
	MuxBackend          string `json:"mux_backend,omitempty"`
	MuxSession          string `json:"mux_session,omitempty"`
	HerdrRelay          bool   `json:"herdr_relay,omitempty"`
	HerdrSession        string `json:"herdr_session,omitempty"`
	PiOperator          bool   `json:"pi_operator,omitempty"`
}

type output struct {
	RunID            string        `json:"run_id"`
	OutputDir        string        `json:"output_dir"`
	Written          bool          `json:"written"`
	ResearchMode     string        `json:"research_mode"`
	Rooms            []roomSpec    `json:"rooms"`
	Agents           []agentSpec   `json:"agents"`
	SocialSkillCalls []skillCall   `json:"social_skill_calls"`
	ExecutionPlan    executionPlan `json:"execution_plan"`
	RoomCommands     []string      `json:"room_commands"`
	Files            []filePlan    `json:"files"`
	Prototype        *prototype    `json:"prototype,omitempty"`
	StateMachine     []string      `json:"state_machine"`
	Rubric           []string      `json:"rubric"`
	MessageContract  string        `json:"message_contract"`
	Warnings         []string      `json:"warnings,omitempty"`
}

type roomSpec struct {
	ID           string   `json:"id"`
	Mode         string   `json:"mode"`
	Purpose      string   `json:"purpose"`
	Agents       []string `json:"agents"`
	Outputs      []string `json:"outputs"`
	ExitCriteria []string `json:"exit_criteria"`
}

type agentSpec struct {
	ID           string   `json:"id"`
	Room         string   `json:"room"`
	Mode         string   `json:"mode"`
	Purpose      string   `json:"purpose"`
	Inputs       []string `json:"inputs"`
	Outputs      []string `json:"outputs"`
	SocialSkills []string `json:"social_skills,omitempty"`
	PromptFile   string   `json:"prompt_file"`
}

type skillCall struct {
	AgentID  string         `json:"agent_id"`
	Skill    string         `json:"skill"`
	Purpose  string         `json:"purpose"`
	Requires []string       `json:"requires,omitempty"`
	Input    map[string]any `json:"input"`
	Command  string         `json:"command"`
}

type filePlan struct {
	Path    string `json:"path"`
	Summary string `json:"summary"`
	Content string `json:"content,omitempty"`
}

type executionOptions struct {
	AgentProvider string
	Provision     bool
	MuxBackend    string
	MuxSession    string
	HerdrRelay    bool
	HerdrSession  string
	PiOperator    bool
}

type executionPlan struct {
	Summary       string              `json:"summary"`
	AgentProvider string              `json:"agent_provider"`
	Provision     bool                `json:"provision"`
	MuxBackend    string              `json:"mux_backend"`
	MuxSession    string              `json:"mux_session,omitempty"`
	Rooms         []roomExecutionPlan `json:"rooms"`
	DebateRoutes  []debateRoute       `json:"debate_routes"`
	GateChecks    []gateCheck         `json:"gate_checks"`
	Commands      []executionCommand  `json:"commands"`
	Herdr         *herdrPlan          `json:"herdr,omitempty"`
	Pi            *piOperatorPlan     `json:"pi,omitempty"`
	Warnings      []string            `json:"warnings,omitempty"`
}

type roomExecutionPlan struct {
	RoomID        string           `json:"room_id"`
	Phase         string           `json:"phase"`
	CreateCommand string           `json:"create_command"`
	LoopCommand   string           `json:"loop_command"`
	RelayCommand  string           `json:"relay_command,omitempty"`
	Members       []roomMemberPlan `json:"members"`
	Tasks         []roomTaskPlan   `json:"tasks"`
	DirectSends   []roomSendPlan   `json:"direct_sends"`
}

type roomMemberPlan struct {
	ActorID    string `json:"actor_id"`
	Role       string `json:"role"`
	Agent      string `json:"agent"`
	Mode       string `json:"mode"`
	PromptFile string `json:"prompt_file,omitempty"`
}

type roomTaskPlan struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Command     string `json:"command"`
}

type roomSendPlan struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
	Command string `json:"command"`
}

type debateRoute struct {
	RoomID        string `json:"room_id"`
	From          string `json:"from"`
	To            string `json:"to"`
	MessageType   string `json:"message_type"`
	Artifact      string `json:"artifact"`
	RequiredReply string `json:"required_reply"`
	Purpose       string `json:"purpose"`
}

type gateCheck struct {
	ID       string   `json:"id"`
	RoomID   string   `json:"room_id"`
	Owner    string   `json:"owner"`
	Criteria []string `json:"criteria"`
}

type executionCommand struct {
	ID      string `json:"id"`
	Phase   string `json:"phase"`
	Summary string `json:"summary"`
	Command string `json:"command"`
}

type herdrPlan struct {
	Enabled  bool     `json:"enabled"`
	Session  string   `json:"session,omitempty"`
	Commands []string `json:"commands,omitempty"`
	Notes    []string `json:"notes,omitempty"`
}

type piOperatorPlan struct {
	Enabled  bool     `json:"enabled"`
	Commands []string `json:"commands,omitempty"`
	Tools    []string `json:"tools,omitempty"`
	Notes    []string `json:"notes,omitempty"`
}

type prototype struct {
	Question        string              `json:"question"`
	MockData        bool                `json:"mock_data"`
	Summary         string              `json:"summary"`
	Stages          []prototypeStage    `json:"stages"`
	MockResearch    []mockResearchSlice `json:"mock_research,omitempty"`
	FinalArtifacts  []string            `json:"final_artifacts"`
	HowToInspect    []string            `json:"how_to_inspect"`
	UnverifiedFacts []string            `json:"unverified_facts,omitempty"`
}

type prototypeStage struct {
	ID              string   `json:"id"`
	Room            string   `json:"room"`
	Agents          []string `json:"agents"`
	InputArtifacts  []string `json:"input_artifacts"`
	OutputArtifacts []string `json:"output_artifacts"`
	Status          string   `json:"status"`
	Demonstrates    string   `json:"demonstrates"`
}

type mockResearchSlice struct {
	AgentID     string   `json:"agent_id"`
	SourceSkill string   `json:"source_skill"`
	Artifact    string   `json:"artifact"`
	Signals     []string `json:"signals"`
	Implication string   `json:"implication"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	blueprint, err := buildBlueprint(in)
	if err != nil {
		return err
	}
	if in.Write {
		if strings.TrimSpace(in.OutputDir) == "" {
			in.OutputDir = "praze-launch"
		}
		_, resolved, err := skillmain.ResolvePath(rc, in.OutputDir)
		if err != nil {
			return err
		}
		if err := writeFiles(resolved, blueprint.Files); err != nil {
			return skillerr.IO("write Praze launch pipeline files", skillerr.WithCause(err))
		}
		blueprint.Written = true
		blueprint.OutputDir = resolved
	}
	if !in.IncludeFileContents {
		for i := range blueprint.Files {
			blueprint.Files[i].Content = ""
		}
	}
	return skillout.EmitWithCAS(ctx, rc, command, blueprint)
}

func buildBlueprint(in input) (output, error) {
	runID := defaultText(in.RunID, "2026-05-praze-launch")
	outputDir := defaultText(in.OutputDir, "praze-launch")
	mode := defaultText(in.ResearchMode, "dry_run")
	if mode != "dry_run" && mode != "live" {
		return output{}, skillerr.Arg("research_mode must be dry_run or live")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	agents := agentSpecs()
	calls := socialSkillCalls(mode == "dry_run", limit)
	execOpts, err := executionOptionsFromInput(in, runID)
	if err != nil {
		return output{}, err
	}
	rooms := roomSpecs()
	execPlan := buildExecutionPlan(runID, rooms, agents, execOpts)
	var proto *prototype
	if in.Prototype {
		mockData := true
		if !in.MockData {
			mockData = true
		}
		proto = prototypeRun(runID, mockData)
	}
	files := pipelineFiles(runID, agents, calls, execPlan, proto)
	return output{
		RunID:            runID,
		OutputDir:        outputDir,
		ResearchMode:     mode,
		Rooms:            rooms,
		Agents:           agents,
		SocialSkillCalls: calls,
		ExecutionPlan:    execPlan,
		RoomCommands:     roomCommandStrings(execPlan.Commands),
		Files:            files,
		Prototype:        proto,
		StateMachine:     []string{"draft", "challenged", "revision_requested", "revised", "approved", "delivered"},
		Rubric: []string{
			"Novelty: does this make Praze feel meaningfully new?",
			"Clarity: would a normal person understand it immediately?",
			"Emotional force: does it make someone feel why this matters?",
			"Product truth: can Praze actually prove or demonstrate this?",
			"Reverence: does it handle prayer with care?",
			"Shareability: would someone repost this because it names something they believe?",
			"Specificity: could only Praze say this?",
			"Friction: does the CTA feel simple and natural?",
			"Risk: could this be misunderstood, mocked, or spiritually manipulative?",
		},
		MessageContract: messageContract(),
		Warnings: []string{
			"Facebook and Instagram Graph calls require approved Meta permissions and concrete Page/IG account IDs.",
			"X cost should be treated as runtime/configured metadata from the X Developer Console, not a compiled constant.",
			"Run research calls in dry_run mode until secrets and source targets are verified.",
			"Herdr is relay-only in this checkout; provision live Codex agents through tmux or zellij, then relay through Herdr only for existing Herdr-bound panes.",
			"Pi room-agile controls require the foxctl daemon endpoint POST /api/rooms/{room_id}/agile and the tracked integrations/pi/foxctl.ts extension.",
		},
	}, nil
}

func roomSpecs() []roomSpec {
	return []roomSpec{
		{
			ID:      "praze-launch-foundation",
			Mode:    "collaborative",
			Purpose: "Create the launch constitution before viral optimization.",
			Agents:  []string{"brand_brief", "keywords", "research_planner"},
			Outputs: []string{"brief/brand-brief.md", "research/keyword-map.md", "research/research-plan.md"},
			ExitCriteria: []string{
				"Forbidden claims are explicit.",
				"Core promise and emotional territory are approved.",
				"Research questions and source targets are assigned before any live social calls.",
			},
		},
		{
			ID:      "praze-launch-research",
			Mode:    "parallel_research",
			Purpose: "Run independent social/API and market research lanes, then compile contradictions.",
			Agents:  []string{"youtube_research", "x_research", "reddit_forum_research", "meta_channel_research", "industry_research", "research_compiler"},
			Outputs: []string{"research/youtube-research.md", "research/x-launch-research.md", "research/reddit-forum-research.md", "research/meta-channel-research.md", "research/industry-research.md", "research/research-compiler.md"},
			ExitCriteria: []string{
				"Every research artifact names source limits and permission constraints.",
				"Compiler resolves conflicts into one market-truth document.",
			},
		},
		{
			ID:      "praze-launch-claim-arena",
			Mode:    "adversarial",
			Purpose: "Generate and attack claims/hooks until novelty, clarity, product truth, and reverence pass.",
			Agents:  []string{"bold_claim", "claim_manager", "counterpositioning", "hook_writer", "hook_manager", "cta_writer", "cta_manager", "healthy_tension_specialist", "technical_specialist", "mom_test"},
			Outputs: []string{"positioning/bold-claims.md", "positioning/claim-manager-review.md", "positioning/counterpositioning.md", "copy/hook-options.md", "copy/hook-manager-review.md", "copy/cta-options.md", "copy/cta-manager-review.md"},
			ExitCriteria: []string{
				"Top hook can be understood without extra explanation.",
				"Claim Manager approves one primary claim and records rejected claims.",
				"CTA Manager approves one primary beta invitation and one secondary action.",
				"Technical Specialist has no blocker findings.",
			},
		},
		{
			ID:      "praze-launch-script-arena",
			Mode:    "adversarial_revision",
			Purpose: "Write, attack, and integrate the post/script/demo narrative.",
			Agents:  []string{"demo_narrative", "body_writer", "conviction_specialist", "healthy_tension_specialist", "technical_specialist", "pastoral_tone", "flow_specialist", "body_manager", "mom_test"},
			Outputs: []string{"copy/demo-narrative.md", "copy/body-drafts.md", "review/conviction-specialist-review.md", "review/healthy-tension-review.md", "review/technical-truth-review.md", "review/pastoral-tone-review.md", "review/flow-specialist-review.md", "copy/body-manager-revision.md"},
			ExitCriteria: []string{
				"Every weak line has a concrete replacement or deletion.",
				"Demo sequence visibly proves the claim.",
				"Pastoral Tone Specialist approves or records only minor edits.",
			},
		},
		{
			ID:      "praze-launch-review-delivery",
			Mode:    "gated_delivery",
			Purpose: "Gate final readiness and package the launch kit.",
			Agents:  []string{"call_supervisor", "final_review", "deliver"},
			Outputs: []string{"review/call-supervisor-report.md", "final/final-review.md", "final/launch-pack/"},
			ExitCriteria: []string{
				"Call Supervisor approves or records only minor edits.",
				"Deliver Agent packages final copy, scripts, reply banks, checklist, metrics, and rejected claims.",
			},
		},
	}
}

func agentSpecs() []agentSpec {
	return []agentSpec{
		agent("brand_brief", "praze-launch-foundation", "collaborative", "Produce launch constitution and forbidden claims.", nil, []string{"brief/brand-brief.md"}),
		agent("keywords", "praze-launch-foundation", "collaborative", "Map audience language and emotional keyword clusters.", nil, []string{"research/keyword-map.md"}),
		agent("research_planner", "praze-launch-foundation", "planner", "Decide source targets, research questions, and dry-run versus live social calls.", []string{"brief/brand-brief.md", "research/keyword-map.md"}, []string{"research/research-plan.md"}),
		agent("youtube_research", "praze-launch-research", "independent_research", "Study video outliers, testimony structures, app-launch demos, and comment language.", []string{"brief/brand-brief.md", "research/keyword-map.md", "research/research-plan.md"}, []string{"research/youtube-research.md"}, "social/youtube_collect"),
		agent("x_research", "praze-launch-research", "independent_research", "Study launch posts, founder hooks, and faith-tech announcement patterns.", []string{"brief/brand-brief.md", "research/keyword-map.md", "research/research-plan.md"}, []string{"research/x-launch-research.md"}, "social/x_collect"),
		agent("reddit_forum_research", "praze-launch-research", "independent_research", "Mine raw pain language, objections, and trust concerns.", []string{"brief/brand-brief.md", "research/keyword-map.md", "research/research-plan.md"}, []string{"research/reddit-forum-research.md"}, "social/reddit_collect"),
		agent("meta_channel_research", "praze-launch-research", "permission_gated_research", "Study approved Facebook Page and Instagram Business/Creator surfaces for churches, ministries, and Christian creators.", []string{"brief/brand-brief.md", "research/keyword-map.md", "research/research-plan.md"}, []string{"research/meta-channel-research.md"}, "social/facebook_collect", "social/instagram_collect"),
		agent("industry_research", "praze-launch-research", "independent_research", "Map competitors, generic claims, and category gaps.", []string{"brief/brand-brief.md", "research/keyword-map.md", "research/research-plan.md"}, []string{"research/industry-research.md"}),
		agent("research_compiler", "praze-launch-research", "synthesizer_adversary", "Turn research into one market-truth document and challenge contradictions.", []string{"research/*.md"}, []string{"research/research-compiler.md"}),
		agent("bold_claim", "praze-launch-claim-arena", "proposal", "Generate and score launch claims.", []string{"research/research-compiler.md", "brief/brand-brief.md"}, []string{"positioning/bold-claims.md"}),
		agent("claim_manager", "praze-launch-claim-arena", "adversarial_manager", "Score claims on novelty, clarity, reverence, emotional force, and proofability.", []string{"positioning/bold-claims.md", "brief/brand-brief.md"}, []string{"positioning/claim-manager-review.md"}),
		agent("counterpositioning", "praze-launch-claim-arena", "adversarial", "Define what Praze is not and remove generic Christian-app positioning.", []string{"positioning/bold-claims.md", "research/research-compiler.md"}, []string{"positioning/counterpositioning.md"}),
		agent("hook_writer", "praze-launch-claim-arena", "proposal", "Write hook variations from approved claims.", []string{"positioning/claim-manager-review.md", "positioning/counterpositioning.md"}, []string{"copy/hook-options.md"}),
		agent("hook_manager", "praze-launch-claim-arena", "adversarial_manager", "Cut generic hooks and select the strongest opening sequence.", []string{"copy/hook-options.md", "positioning/claim-manager-review.md"}, []string{"copy/hook-manager-review.md"}),
		agent("cta_writer", "praze-launch-claim-arena", "proposal", "Write beta/founding-intercessor CTAs without gimmicks.", []string{"brief/brand-brief.md"}, []string{"copy/cta-options.md"}),
		agent("cta_manager", "praze-launch-claim-arena", "adversarial_manager", "Select primary/secondary/comment/landing CTAs and measurement notes.", []string{"copy/cta-options.md"}, []string{"copy/cta-manager-review.md"}),
		agent("demo_narrative", "praze-launch-script-arena", "proposal", "Define the visual proof sequence: request, map/pulse, voice prayer, moderation/translation, delivered encouragement.", []string{"positioning/claim-manager-review.md", "research/research-compiler.md"}, []string{"copy/demo-narrative.md"}),
		agent("body_writer", "praze-launch-script-arena", "proposal", "Write launch posts and video scripts that prove the claim.", []string{"copy/hook-manager-review.md", "copy/cta-manager-review.md", "copy/demo-narrative.md"}, []string{"copy/body-drafts.md"}),
		agent("conviction_specialist", "praze-launch-script-arena", "adversarial", "Attack weak lines, filler, generic phrasing, and unreverent intensity.", []string{"copy/body-drafts.md"}, []string{"review/conviction-specialist-review.md"}),
		agent("healthy_tension_specialist", "praze-launch-script-arena", "adversarial", "Sharpen fair tension without attacking churches or exploiting pain.", []string{"copy/body-drafts.md", "positioning/bold-claims.md"}, []string{"review/healthy-tension-review.md"}),
		agent("technical_specialist", "praze-launch-script-arena", "fact_checker", "Verify product, moderation, AI, privacy, safety, beta, and availability claims.", []string{"brief/product-facts.md", "copy/body-drafts.md"}, []string{"review/technical-truth-review.md"}),
		agent("pastoral_tone", "praze-launch-script-arena", "adversarial", "Check spiritual responsibility, reverence, and anti-manipulation tone.", []string{"copy/body-drafts.md", "review/technical-truth-review.md"}, []string{"review/pastoral-tone-review.md"}),
		agent("flow_specialist", "praze-launch-script-arena", "adversarial_editor", "Fix sequence, cognitive load, visual proof, and demo pacing.", []string{"copy/body-drafts.md", "review/technical-truth-review.md"}, []string{"review/flow-specialist-review.md"}),
		agent("body_manager", "praze-launch-script-arena", "integrator", "Integrate critiques into the revised launch pack.", []string{"review/*.md", "copy/body-drafts.md", "copy/demo-narrative.md"}, []string{"copy/body-manager-revision.md"}),
		agent("mom_test", "praze-launch-script-arena", "clarity_tester", "Test plain-English comprehension and rewrite confusing lines.", []string{"copy/body-manager-revision.md"}, []string{"review/mom-test-review.md"}),
		agent("call_supervisor", "praze-launch-review-delivery", "gatekeeper", "Approve, send back, or block the launch package.", []string{"copy/body-manager-revision.md", "review/mom-test-review.md", "review/technical-truth-review.md"}, []string{"review/call-supervisor-report.md"}),
		agent("final_review", "praze-launch-review-delivery", "final_editor", "Final taste, founder voice, theological responsibility, and polish.", []string{"review/call-supervisor-report.md", "copy/body-manager-revision.md"}, []string{"final/final-review.md"}),
		agent("deliver", "praze-launch-review-delivery", "packager", "Package final launch copy, scripts, reply banks, checklist, metrics, and rejected claims.", []string{"final/final-review.md"}, []string{"final/launch-pack/"}),
	}
}

func agent(id, room, mode, purpose string, inputs, outputs []string, socialSkills ...string) agentSpec {
	if inputs == nil {
		inputs = []string{}
	}
	if outputs == nil {
		outputs = []string{}
	}
	return agentSpec{
		ID:           id,
		Room:         room,
		Mode:         mode,
		Purpose:      purpose,
		Inputs:       inputs,
		Outputs:      outputs,
		SocialSkills: socialSkills,
		PromptFile:   filepath.ToSlash(filepath.Join("agents", id+".md")),
	}
}

func socialSkillCalls(dryRun bool, limit int) []skillCall {
	calls := []skillCall{
		call("youtube_research", "social/youtube_collect", "Find YouTube videos around prayer testimony and app-launch structures.", nil, map[string]any{
			"operation": "search", "query": "prayer testimony Christian app launch global prayer", "type": "video", "limit": limit, "dry_run": dryRun,
		}),
		call("youtube_research", "social/youtube_collect", "Hydrate selected video IDs after search.", []string{"Replace <video_id> with IDs from the search result."}, map[string]any{
			"operation": "videos", "ids": []string{"<video_id>"}, "dry_run": dryRun,
		}),
		call("youtube_research", "social/youtube_collect", "Inspect comment language for selected outlier videos.", []string{"Replace <video_id> with a selected video ID."}, map[string]any{
			"operation": "comments", "video_id": "<video_id>", "limit": limit, "dry_run": dryRun,
		}),
		call("x_research", "social/x_collect", "Study launch posts and hook patterns around consumer, audio, map, and faith-tech launches.", nil, map[string]any{
			"operation": "recent_search", "query": `("launching" OR "launched") ("prayer app" OR "Christian app" OR "faith tech" OR "audio app")`, "limit": limit, "dry_run": dryRun,
		}),
		call("x_research", "social/x_collect", "Measure query volume before spending reads on broad searches.", nil, map[string]any{
			"operation": "post_counts", "query": `"prayer requests" ("feed" OR "app" OR "community")`, "dry_run": dryRun,
		}),
		call("reddit_forum_research", "social/reddit_collect", "Collect raw prayer request language from a subreddit search.", nil, map[string]any{
			"operation": "subreddit_search", "subreddit": "PrayerRequests", "query": `"please pray" OR "pray for me"`, "sort": "relevance", "time_filter": "year", "limit": limit, "dry_run": dryRun,
		}),
		call("reddit_forum_research", "social/reddit_collect", "Collect objections and trust concerns around prayer apps or online Christian communities.", nil, map[string]any{
			"operation": "subreddit_search", "subreddit": "Christianity", "query": `"prayer app" OR "online prayer" OR "prayer request"`, "sort": "relevance", "time_filter": "year", "limit": limit, "dry_run": dryRun,
		}),
		call("meta_channel_research", "social/facebook_collect", "Study approved Facebook Page posts for churches, ministries, or prayer organizations.", []string{"Requires approved Page access and a real page_id."}, map[string]any{
			"operation": "page_posts", "page_id": "<facebook_page_id>", "limit": limit, "dry_run": dryRun,
		}),
		call("meta_channel_research", "social/facebook_collect", "Inspect comments on an approved Facebook Page post.", []string{"Requires approved Page/comment permissions and a real post_id."}, map[string]any{
			"operation": "post_comments", "post_id": "<facebook_post_id>", "limit": limit, "dry_run": dryRun,
		}),
		call("meta_channel_research", "social/instagram_collect", "Use Instagram Business Discovery for approved Business/Creator targets.", []string{"Requires an authorized ig_user_id and target Business/Creator username."}, map[string]any{
			"operation": "business_discovery", "ig_user_id": "<own_ig_user_id>", "username": "<target_business_username>", "dry_run": dryRun,
		}),
		call("meta_channel_research", "social/instagram_collect", "Study owned or authorized Instagram media and captions.", []string{"Requires an authorized IG Business/Creator user ID."}, map[string]any{
			"operation": "user_media", "ig_user_id": "<own_ig_user_id>", "limit": limit, "dry_run": dryRun,
		}),
	}
	return calls
}

func call(agentID, skill, purpose string, requires []string, input map[string]any) skillCall {
	return skillCall{
		AgentID:  agentID,
		Skill:    skill,
		Purpose:  purpose,
		Requires: requires,
		Input:    input,
		Command:  "foxctl run " + skill + " --input '" + compactJSON(input) + "'",
	}
}

func compactJSON(value any) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "{}"
	}
	return strings.TrimSpace(buf.String())
}

func executionOptionsFromInput(in input, runID string) (executionOptions, error) {
	opts := executionOptions{
		AgentProvider: defaultText(in.AgentProvider, "codex"),
		Provision:     in.Provision,
		MuxBackend:    defaultText(in.MuxBackend, "auto"),
		MuxSession:    in.MuxSession,
		HerdrRelay:    in.HerdrRelay,
		HerdrSession:  in.HerdrSession,
		PiOperator:    in.PiOperator,
	}
	switch opts.MuxBackend {
	case "auto", "tmux", "zellij":
	default:
		return executionOptions{}, skillerr.Arg("mux_backend must be auto, tmux, or zellij", skillerr.WithHint("Use herdr_relay:true for Herdr; room create provisioning does not support --mux-backend herdr in this checkout."))
	}
	if opts.MuxSession == "" {
		opts.MuxSession = runID + "-agents"
	}
	if opts.HerdrRelay && opts.HerdrSession == "" {
		opts.HerdrSession = runID
	}
	return opts, nil
}

func buildExecutionPlan(runID string, rooms []roomSpec, agents []agentSpec, opts executionOptions) executionPlan {
	agentByID := map[string]agentSpec{}
	for _, agent := range agents {
		agentByID[agent.ID] = agent
	}
	routes := debateRoutes()
	gates := gateChecks()
	plan := executionPlan{
		Summary:       "Durable foxctl rooms carry the debate. Codex agents are provisioned as room participants when provision:true, and every critique/revision travels through directed room messages rather than pane scrollback.",
		AgentProvider: opts.AgentProvider,
		Provision:     opts.Provision,
		MuxBackend:    opts.MuxBackend,
		MuxSession:    opts.MuxSession,
		DebateRoutes:  routes,
		GateChecks:    gates,
		Warnings: []string{
			"Keep Product Facts as the authority for product claims before final copy.",
			"Use room loop as the durable delivery/retry layer; panes are inspection hosts only.",
		},
	}
	if opts.HerdrRelay {
		plan.Herdr = &herdrPlan{
			Enabled: true,
			Session: opts.HerdrSession,
			Notes: []string{
				"Herdr is available through room relay, not room create provisioning.",
				"Use only after participants have delivery bindings that resolve to Herdr panes.",
			},
		}
	}
	if opts.PiOperator {
		plan.Pi = &piOperatorPlan{
			Enabled: true,
			Tools: []string{
				"foxctl_rooms_list",
				"foxctl_room_detail",
				"foxctl_room_messages",
				"foxctl_room_send",
				"foxctl_room_agile",
				"foxctl_story_start",
				"foxctl_story_review",
				"foxctl_story_validate",
				"foxctl_agent_spawn",
			},
			Notes: []string{
				"Pi can act as the human operator console for room status, room-agile epic state, story lifecycle, messages, and intervention.",
				"Start or select the room-agile epic first, then launch Pi with --foxctl-room and --foxctl-epic so /epic, /epic-next, and the epic widget are scoped.",
			},
		}
	}
	for i, room := range rooms {
		roomPlan := buildRoomExecutionPlan(runID, room, agentByID, opts, i+1)
		plan.Rooms = append(plan.Rooms, roomPlan)
		plan.Commands = append(plan.Commands, executionCommand{
			ID:      fmt.Sprintf("create-%02d-%s", i+1, room.ID),
			Phase:   "room-create",
			Summary: "Create or update " + room.ID,
			Command: roomPlan.CreateCommand,
		})
		for j, task := range roomPlan.Tasks {
			plan.Commands = append(plan.Commands, executionCommand{
				ID:      fmt.Sprintf("task-%02d-%02d-%s", i+1, j+1, room.ID),
				Phase:   "room-task",
				Summary: task.Title,
				Command: task.Command,
			})
		}
		for j, send := range roomPlan.DirectSends {
			plan.Commands = append(plan.Commands, executionCommand{
				ID:      fmt.Sprintf("send-%02d-%02d-%s", i+1, j+1, send.To),
				Phase:   "room-send",
				Summary: send.Subject,
				Command: send.Command,
			})
		}
		plan.Commands = append(plan.Commands, executionCommand{
			ID:      fmt.Sprintf("loop-%02d-%s", i+1, room.ID),
			Phase:   "room-loop",
			Summary: "Run delivery and stale-work loop for " + room.ID,
			Command: roomPlan.LoopCommand,
		})
		if roomPlan.RelayCommand != "" {
			plan.Commands = append(plan.Commands, executionCommand{
				ID:      fmt.Sprintf("relay-%02d-%s", i+1, room.ID),
				Phase:   "room-relay",
				Summary: "Relay " + room.ID + " through Herdr",
				Command: roomPlan.RelayCommand,
			})
			if plan.Herdr != nil {
				plan.Herdr.Commands = append(plan.Herdr.Commands, roomPlan.RelayCommand)
			}
		}
	}
	if plan.Pi != nil {
		epicStart := buildPrazeEpicStartCommand(runID)
		plan.Commands = append(plan.Commands, executionCommand{
			ID:      "epic-start-praze-launch",
			Phase:   "room-agile",
			Summary: "Start a room-agile epic for the Praze launch pipeline",
			Command: epicStart,
		})
		plan.Pi.Commands = []string{
			epicStart,
			"pi --extension integrations/pi/foxctl.ts --foxctl-url http://localhost:8090 --foxctl-workspace . --foxctl-room praze-launch-review-delivery --foxctl-room-bind --foxctl-context --foxctl-epic-context",
			"/epic-select",
			"/epic",
			"/epic-next",
			"/epic-health",
			"/milestones",
			"/stories",
			"/foxctl-room-status",
			"/foxctl-room-inbox",
		}
	}
	return plan
}

func buildPrazeEpicStartCommand(runID string) string {
	return shellCommand(
		"foxctl", "room", "epic", "start", "praze-launch-review-delivery", "Praze Launch Pipeline",
		"--workspace", ".",
		"--sender", "call_supervisor",
		"--owner", "call_supervisor",
		"--goal", "Run the Praze adversarial launch pipeline from social research through final launch delivery.",
		"--outcome", "Approved launch pack with verified product claims, final copy, scripts, reply banks, metrics, and rejected claims archive.",
		"--horizon", runID,
		"--scope", "praze-launch-foundation",
		"--scope", "praze-launch-research",
		"--scope", "praze-launch-claim-arena",
		"--scope", "praze-launch-script-arena",
		"--scope", "praze-launch-review-delivery",
		"--success", "Call Supervisor approves with no blocker findings.",
		"--success", "Technical Specialist records no product-truth blockers.",
		"--success", "Deliver Agent packages final/launch-pack.",
	)
}

func buildRoomExecutionPlan(runID string, room roomSpec, agentByID map[string]agentSpec, opts executionOptions, order int) roomExecutionPlan {
	members := roomMembersForExecution(room.Agents, agentByID, opts.AgentProvider)
	create := buildRoomCreateCommand(room, members, opts)
	loop := "foxctl room loop " + shellQuote(room.ID) + " --workspace . --backend " + shellQuote(opts.MuxBackend)
	relay := ""
	if opts.HerdrRelay {
		relay = "foxctl room relay " + shellQuote(room.ID) + " --workspace . --backend herdr --session " + shellQuote(opts.HerdrSession)
	}
	task := roomTaskPlan{
		Title:       fmt.Sprintf("Run %02d %s for %s", order, room.Mode, runID),
		Description: "Use the room message contract, write declared artifacts, and send directed critiques to the next responsible agent.",
	}
	task.Command = "foxctl room task add " + shellQuote(room.ID) + " --workspace . --sender call_supervisor --title " + shellQuote(task.Title) + " --description " + shellQuote(task.Description)
	sends := make([]roomSendPlan, 0, len(room.Agents))
	for _, agentID := range room.Agents {
		promptFile := promptFileForAgent(agentID, agentByID)
		text := "Read agents/00-shared-constitution.md, rooms/message-contract.md, and " + promptFile + ". Produce or review your declared outputs, then reply in the room message contract with the next agent named."
		subject := "Praze pipeline assignment: " + agentID
		sends = append(sends, roomSendPlan{
			To:      agentID,
			Subject: subject,
			Text:    text,
			Command: "foxctl room send " + shellQuote(room.ID) + " --workspace . --sender call_supervisor --to " + shellQuote(agentID) + " --reply-expected --kind instruction --subject " + shellQuote(subject) + " " + shellQuote(text),
		})
	}
	return roomExecutionPlan{
		RoomID:        room.ID,
		Phase:         room.Mode,
		CreateCommand: create,
		LoopCommand:   loop,
		RelayCommand:  relay,
		Members:       members,
		Tasks:         []roomTaskPlan{task},
		DirectSends:   sends,
	}
}

func roomMembersForExecution(agentIDs []string, agentByID map[string]agentSpec, provider string) []roomMemberPlan {
	members := []roomMemberPlan{{
		ActorID: "call_supervisor",
		Role:    "coordinator",
		Agent:   provider,
		Mode:    "auto",
	}}
	for _, agentID := range agentIDs {
		if agentID == "call_supervisor" {
			continue
		}
		members = append(members, roomMemberPlan{
			ActorID:    agentID,
			Role:       roomRoleForAgent(agentID),
			Agent:      provider,
			Mode:       "auto",
			PromptFile: promptFileForAgent(agentID, agentByID),
		})
	}
	return members
}

func promptFileForAgent(agentID string, agentByID map[string]agentSpec) string {
	if spec, ok := agentByID[agentID]; ok && spec.PromptFile != "" {
		return spec.PromptFile
	}
	return filepath.ToSlash(filepath.Join("agents", agentID+".md"))
}

func roomRoleForAgent(agentID string) string {
	roles := map[string]string{
		"brand_brief":                "planner",
		"keywords":                   "researcher",
		"research_planner":           "planner",
		"youtube_research":           "researcher",
		"x_research":                 "researcher",
		"reddit_forum_research":      "researcher",
		"meta_channel_research":      "researcher",
		"industry_research":          "researcher",
		"research_compiler":          "reviewer",
		"bold_claim":                 "planner",
		"claim_manager":              "reviewer",
		"counterpositioning":         "reviewer",
		"hook_writer":                "writer",
		"hook_manager":               "reviewer",
		"cta_writer":                 "writer",
		"cta_manager":                "reviewer",
		"healthy_tension_specialist": "reviewer",
		"technical_specialist":       "reviewer",
		"mom_test":                   "reviewer",
		"demo_narrative":             "planner",
		"body_writer":                "writer",
		"conviction_specialist":      "reviewer",
		"pastoral_tone":              "reviewer",
		"flow_specialist":            "reviewer",
		"body_manager":               "planner",
		"call_supervisor":            "coordinator",
		"final_review":               "reviewer",
		"deliver":                    "planner",
	}
	if role, ok := roles[agentID]; ok {
		return role
	}
	return "member"
}

func buildRoomCreateCommand(room roomSpec, members []roomMemberPlan, opts executionOptions) string {
	args := []string{
		"foxctl", "room", "create", room.ID,
		"--workspace", ".",
		"--title", titleForRoom(room.ID),
		"--description", room.Purpose,
	}
	if opts.Provision {
		args = append(
			args,
			"--provision",
			"--agent", opts.AgentProvider,
			"--mode", "auto",
			"--mux-backend", opts.MuxBackend,
			"--mux-session", opts.MuxSession,
		)
	}
	for _, member := range members {
		args = append(args, "--member", member.ActorID+"="+member.Role+"@"+member.Agent+":"+member.Mode)
	}
	return shellCommand(args...)
}

func titleForRoom(roomID string) string {
	titles := map[string]string{
		"praze-launch-foundation":      "Praze Launch Foundation",
		"praze-launch-research":        "Praze Launch Research",
		"praze-launch-claim-arena":     "Praze Claim Arena",
		"praze-launch-script-arena":    "Praze Script Arena",
		"praze-launch-review-delivery": "Praze Review Delivery",
	}
	if title, ok := titles[roomID]; ok {
		return title
	}
	return roomID
}

func debateRoutes() []debateRoute {
	return []debateRoute{
		route("praze-launch-foundation", "research_planner", "youtube_research", "ARTIFACT", "research/research-plan.md", "source targets and dry-run call decisions", "Start video research from approved questions."),
		route("praze-launch-foundation", "research_planner", "x_research", "ARTIFACT", "research/research-plan.md", "source targets and dry-run call decisions", "Start X launch research from approved questions."),
		route("praze-launch-foundation", "research_planner", "reddit_forum_research", "ARTIFACT", "research/research-plan.md", "source targets and dry-run call decisions", "Start Reddit/forum language research from approved questions."),
		route("praze-launch-foundation", "research_planner", "meta_channel_research", "ARTIFACT", "research/research-plan.md", "approved Page/IG targets or permission-gated skip decision", "Start Meta channel research only from approved targets."),
		route("praze-launch-foundation", "research_planner", "industry_research", "ARTIFACT", "research/research-plan.md", "competitor/category questions and source list", "Start category and competitor positioning research."),
		route("praze-launch-research", "youtube_research", "research_compiler", "ARTIFACT", "research/youtube-research.md", "compiler accepts source limits or asks for revision", "Compile video structures into market truth."),
		route("praze-launch-research", "x_research", "research_compiler", "ARTIFACT", "research/x-launch-research.md", "compiler accepts source limits or asks for revision", "Compile launch-post patterns into market truth."),
		route("praze-launch-research", "reddit_forum_research", "research_compiler", "ARTIFACT", "research/reddit-forum-research.md", "compiler accepts source limits or asks for revision", "Compile raw pain language and objections."),
		route("praze-launch-research", "meta_channel_research", "research_compiler", "ARTIFACT", "research/meta-channel-research.md", "compiler accepts permission limits or asks for revision", "Compile Meta channel findings without overclaiming."),
		route("praze-launch-research", "industry_research", "research_compiler", "ARTIFACT", "research/industry-research.md", "compiler accepts category map or asks for revision", "Compile competitor gaps and generic claims."),
		route("praze-launch-claim-arena", "bold_claim", "claim_manager", "ARTIFACT", "positioning/bold-claims.md", "APPROVAL or REVISION_REQUEST with scored replacements", "Pick one claim that survives novelty, clarity, reverence, and proofability."),
		route("praze-launch-claim-arena", "counterpositioning", "claim_manager", "CRITIQUE", "positioning/counterpositioning.md", "counterpositioning incorporated or rejected with reason", "Remove generic Christian-app territory."),
		route("praze-launch-claim-arena", "claim_manager", "hook_writer", "APPROVAL", "positioning/claim-manager-review.md", "approved claim and rejected-claim notes", "Write hooks from the approved claim only."),
		route("praze-launch-claim-arena", "hook_writer", "hook_manager", "ARTIFACT", "copy/hook-options.md", "top hooks plus rejected hook log", "Cut weak hooks and select the opening sequence."),
		route("praze-launch-claim-arena", "hook_manager", "demo_narrative", "APPROVAL", "copy/hook-manager-review.md", "approved opening sequence", "Design the proof sequence around the selected hook."),
		route("praze-launch-claim-arena", "cta_writer", "cta_manager", "ARTIFACT", "copy/cta-options.md", "primary and secondary CTA selected", "Keep the invitation clear and spiritually responsible."),
		route("praze-launch-claim-arena", "cta_manager", "body_writer", "APPROVAL", "copy/cta-manager-review.md", "approved primary and secondary CTAs", "Use the approved invitation in launch copy."),
		route("praze-launch-script-arena", "demo_narrative", "body_writer", "ARTIFACT", "copy/demo-narrative.md", "proof sequence accepted or revised by Body Writer", "Write copy around visible proof moments."),
		route("praze-launch-script-arena", "body_writer", "conviction_specialist", "ARTIFACT", "copy/body-drafts.md", "line-level patch or approval", "Attack filler and generic copy."),
		route("praze-launch-script-arena", "body_writer", "technical_specialist", "ARTIFACT", "copy/body-drafts.md", "VERIFIED, UNVERIFIED, TOO STRONG, or BLOCKER for every claim", "Prevent false product, safety, AI, privacy, and launch claims."),
		route("praze-launch-script-arena", "body_writer", "pastoral_tone", "ARTIFACT", "copy/body-drafts.md", "tone patch or approval", "Protect reverence and pastoral responsibility."),
		route("praze-launch-script-arena", "body_writer", "flow_specialist", "ARTIFACT", "copy/body-drafts.md", "reordered sequence or approval", "Make the demo prove the claim in the right order."),
		route("praze-launch-script-arena", "conviction_specialist", "body_manager", "CRITIQUE", "review/conviction-specialist-review.md", "patch accepted or rejected with reason", "Integrate line-level critique."),
		route("praze-launch-script-arena", "healthy_tension_specialist", "body_manager", "CRITIQUE", "review/healthy-tension-review.md", "tension patches accepted or rejected with reason", "Integrate fair tension without combative copy."),
		route("praze-launch-script-arena", "technical_specialist", "body_manager", "BLOCKER", "review/technical-truth-review.md", "all blockers resolved before final review", "Integrate product-truth constraints."),
		route("praze-launch-script-arena", "pastoral_tone", "body_manager", "CRITIQUE", "review/pastoral-tone-review.md", "tone patches accepted or rejected with reason", "Integrate spiritual-responsibility constraints."),
		route("praze-launch-script-arena", "flow_specialist", "body_manager", "CRITIQUE", "review/flow-specialist-review.md", "sequence patch accepted or rejected with reason", "Integrate demo pacing and ordering constraints."),
		route("praze-launch-script-arena", "body_manager", "mom_test", "ARTIFACT", "copy/body-manager-revision.md", "plain-English comprehension pass or revision request", "Check the integrated draft before supervisor review."),
		route("praze-launch-script-arena", "mom_test", "body_manager", "CRITIQUE", "review/mom-test-review.md", "plain-English rewrite incorporated", "Resolve confusion before supervisor gate."),
		route("praze-launch-review-delivery", "body_manager", "call_supervisor", "ARTIFACT", "copy/body-manager-revision.md", "APPROVAL, minor edits, send-back, or block", "Submit the integrated launch package for readiness gating."),
		route("praze-launch-review-delivery", "call_supervisor", "final_review", "APPROVAL", "review/call-supervisor-report.md", "final edit only after supervisor approves", "Run final editorial review."),
		route("praze-launch-review-delivery", "final_review", "deliver", "APPROVAL", "final/final-review.md", "launch pack written and rejected angles archived", "Package the launch kit."),
	}
}

func route(roomID, from, to, messageType, artifact, requiredReply, purpose string) debateRoute {
	return debateRoute{
		RoomID:        roomID,
		From:          from,
		To:            to,
		MessageType:   messageType,
		Artifact:      artifact,
		RequiredReply: requiredReply,
		Purpose:       purpose,
	}
}

func gateChecks() []gateCheck {
	return []gateCheck{
		{
			ID:     "foundation-ready",
			RoomID: "praze-launch-foundation",
			Owner:  "research_planner",
			Criteria: []string{
				"Brand brief names forbidden claims.",
				"Keyword map separates emotional language from SEO terms.",
				"Research plan assigns dry-run social calls before live API use.",
			},
		},
		{
			ID:     "research-compiled",
			RoomID: "praze-launch-research",
			Owner:  "research_compiler",
			Criteria: []string{
				"Every social artifact states source and API limits.",
				"Contradictions are resolved into one market-truth document.",
				"Mocked data is labeled when credentials are unavailable.",
			},
		},
		{
			ID:     "claim-approved",
			RoomID: "praze-launch-claim-arena",
			Owner:  "claim_manager",
			Criteria: []string{
				"Primary claim scores at least 4 on novelty, clarity, reverence, and proofability.",
				"Counterpositioning removes generic Christian-social language.",
				"Hook Manager selects one opening sequence and records rejected hooks.",
			},
		},
		{
			ID:     "script-cleared",
			RoomID: "praze-launch-script-arena",
			Owner:  "body_manager",
			Criteria: []string{
				"Technical Specialist has no blocker findings.",
				"Pastoral Tone has no blocker findings.",
				"Mom Test can explain Praze in plain English.",
			},
		},
		{
			ID:     "delivery-approved",
			RoomID: "praze-launch-review-delivery",
			Owner:  "call_supervisor",
			Criteria: []string{
				"Supervisor approves or records only minor edits.",
				"Final Review has no product-truth or tone blockers.",
				"Deliver packages launch copy, scripts, replies, metrics, and rejected claims.",
			},
		},
	}
}

func roomCommandStrings(commands []executionCommand) []string {
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		out = append(out, command.Command)
	}
	return out
}

func shellCommand(args ...string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '_' || r == '-' || r == ':' || r == '=' || r == '@' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'))
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func pipelineFiles(runID string, agents []agentSpec, calls []skillCall, execPlan executionPlan, proto *prototype) []filePlan {
	files := []filePlan{
		{
			Path:    "README.md",
			Summary: "Room execution guide and artifact map.",
			Content: readme(runID),
		},
		{
			Path:    "brief/brand-brief.md",
			Summary: "Brand constitution skeleton.",
			Content: brandBrief(),
		},
		{
			Path:    "brief/product-facts.md",
			Summary: "Claims that must be verified before launch copy can use them.",
			Content: productFacts(),
		},
		{
			Path:    "brief/proof-library.md",
			Summary: "Visual/product proof moments that launch copy may reference after verification.",
			Content: proofLibrary(),
		},
		{
			Path:    "brief/forbidden-claims.md",
			Summary: "Hard rejection rules for claims and hooks.",
			Content: forbiddenClaims(),
		},
		{
			Path:    "research/research-plan.md",
			Summary: "Research planner handoff for source targets and live-call gates.",
			Content: researchPlan(calls),
		},
		{
			Path:    "research/social-skill-callbook.md",
			Summary: "Concrete social research calls mapped to pipeline agents.",
			Content: socialCallbook(calls),
		},
		{
			Path:    "agents/00-shared-constitution.md",
			Summary: "Shared prompt all agents must obey.",
			Content: sharedConstitution(),
		},
		{
			Path:    "rooms/message-contract.md",
			Summary: "Room message contract, scoring, and adversarial patch rule.",
			Content: messageContract(),
		},
		{
			Path:    "rooms/codex-execution-plan.md",
			Summary: "Codex-agent room creation, assignment, loop, and relay runbook.",
			Content: executionPlanMarkdown(execPlan),
		},
		{
			Path:    "rooms/debate-routes.md",
			Summary: "Directed agent debate routes and gate checks.",
			Content: debateRoutesMarkdown(execPlan),
		},
		{
			Path:    "rooms/pi-herdr-ops.md",
			Summary: "Optional Pi and Herdr operation notes for the launch rooms.",
			Content: piHerdrOpsMarkdown(execPlan),
		},
		{
			Path:    "runs/" + runID + "/00-run-index.md",
			Summary: "Run-local artifact checklist.",
			Content: runIndex(runID, agents),
		},
	}
	if proto != nil {
		files = append(files, prototypeFiles(runID, *proto)...)
	}
	for _, spec := range agents {
		files = append(files, filePlan{
			Path:    spec.PromptFile,
			Summary: spec.Purpose,
			Content: agentPrompt(spec, calls),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func writeFiles(root string, files []filePlan) error {
	for _, file := range files {
		target := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(file.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func readme(runID string) string {
	return fmt.Sprintf(`# Praze Launch Pipeline

Run ID: %s

Purpose: run a room-based adversarial launch pipeline for Praze where direct social API research feeds the claim, hook, script, and review stages.

Core claim seed:

> Prayer requests should not disappear into a feed. They should become care you can hear.

Use `+"`research/social-skill-callbook.md`"+` before research agents write conclusions. Start in dry-run mode until credentials, source targets, and API access are verified.

Use `+"`rooms/codex-execution-plan.md`"+` to create durable foxctl rooms and optionally provision live Codex agents. The room timeline is the source of truth; pane scrollback is only a viewer.

Recommended room order:

1. Foundation
2. Research
3. Claim Arena
4. Script Arena
5. Review Delivery

Artifact state machine:

`+"```txt"+`
draft -> challenged -> revision_requested -> revised -> approved -> delivered
`+"```"+`
`, runID)
}

func executionPlanMarkdown(plan executionPlan) string {
	var b strings.Builder
	b.WriteString("# Codex Execution Plan\n\n")
	fmt.Fprintf(&b, "Summary: %s\n\n", plan.Summary)
	fmt.Fprintf(&b, "- Agent provider: `%s`\n", plan.AgentProvider)
	fmt.Fprintf(&b, "- Provision live panes: `%t`\n", plan.Provision)
	fmt.Fprintf(&b, "- Mux backend: `%s`\n", plan.MuxBackend)
	fmt.Fprintf(&b, "- Mux session: `%s`\n\n", plan.MuxSession)
	b.WriteString("## Operating Rules\n\n")
	b.WriteString("- Create rooms first, then assign agents with directed `room send --to` messages.\n")
	b.WriteString("- Run `room loop` for delivery, reminders, task status, and stale-work nudges.\n")
	b.WriteString("- Treat `room status`, `room inbox`, and artifacts on disk as the audit trail.\n")
	b.WriteString("- Do not rely on tmux, zellij, or Herdr scrollback as canonical history.\n\n")
	b.WriteString("## Commands\n\n")
	for _, command := range plan.Commands {
		fmt.Fprintf(&b, "### %s\n\n%s\n\n```bash\n%s\n```\n\n", command.ID, command.Summary, command.Command)
	}
	b.WriteString("## Room Members\n\n")
	for _, room := range plan.Rooms {
		fmt.Fprintf(&b, "### %s\n\n", room.RoomID)
		for _, member := range room.Members {
			fmt.Fprintf(&b, "- `%s` as `%s` via `%s:%s`", member.ActorID, member.Role, member.Agent, member.Mode)
			if member.PromptFile != "" {
				fmt.Fprintf(&b, " using `%s`", member.PromptFile)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(plan.Warnings) > 0 {
		b.WriteString("## Warnings\n\n")
		for _, warning := range plan.Warnings {
			fmt.Fprintf(&b, "- %s\n", warning)
		}
	}
	return b.String()
}

func debateRoutesMarkdown(plan executionPlan) string {
	var b strings.Builder
	b.WriteString("# Debate Routes\n\n")
	b.WriteString("Every adversarial message is directed. Rejections must include a patch: weak line, failure reason, replacement, and severity.\n\n")
	b.WriteString("## Routes\n\n")
	for _, route := range plan.DebateRoutes {
		fmt.Fprintf(&b, "### %s -> %s\n\n", route.From, route.To)
		fmt.Fprintf(&b, "- Room: `%s`\n", route.RoomID)
		fmt.Fprintf(&b, "- Type: `%s`\n", route.MessageType)
		fmt.Fprintf(&b, "- Artifact: `%s`\n", route.Artifact)
		fmt.Fprintf(&b, "- Required reply: %s\n", route.RequiredReply)
		fmt.Fprintf(&b, "- Purpose: %s\n\n", route.Purpose)
	}
	b.WriteString("## Gates\n\n")
	for _, gate := range plan.GateChecks {
		fmt.Fprintf(&b, "### %s\n\nOwner: `%s`\n\nCriteria:\n", gate.ID, gate.Owner)
		for _, criterion := range gate.Criteria {
			fmt.Fprintf(&b, "- %s\n", criterion)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func piHerdrOpsMarkdown(plan executionPlan) string {
	var b strings.Builder
	b.WriteString("# Pi and Herdr Ops\n\n")
	b.WriteString("This file records optional operator and relay surfaces for the Praze launch rooms.\n\n")
	b.WriteString("## Herdr\n\n")
	if plan.Herdr == nil || !plan.Herdr.Enabled {
		b.WriteString("Herdr relay is disabled for this generated run. Re-run with `herdr_relay:true` to emit relay commands.\n\n")
	} else {
		fmt.Fprintf(&b, "Session: `%s`\n\n", plan.Herdr.Session)
		for _, note := range plan.Herdr.Notes {
			fmt.Fprintf(&b, "- %s\n", note)
		}
		b.WriteString("\nCommands:\n\n")
		for _, command := range plan.Herdr.Commands {
			fmt.Fprintf(&b, "```bash\n%s\n```\n\n", command)
		}
	}
	b.WriteString("## Pi\n\n")
	if plan.Pi == nil || !plan.Pi.Enabled {
		b.WriteString("Pi operator mode is disabled for this generated run. Re-run with `pi_operator:true` to emit Pi launch commands and tool expectations.\n")
		return b.String()
	}
	b.WriteString("Expected Pi tools:\n")
	for _, tool := range plan.Pi.Tools {
		fmt.Fprintf(&b, "- `%s`\n", tool)
	}
	b.WriteString("\nCommands:\n\n")
	for _, command := range plan.Pi.Commands {
		fmt.Fprintf(&b, "```bash\n%s\n```\n\n", command)
	}
	b.WriteString("Notes:\n")
	for _, note := range plan.Pi.Notes {
		fmt.Fprintf(&b, "- %s\n", note)
	}
	b.WriteString("\nRoom-agile slash commands:\n")
	b.WriteString("- `/epic` shows the configured epic or lists epics when unset.\n")
	b.WriteString("- `/epic-next` shows the next room-agile actions for the configured epic.\n")
	b.WriteString("- `/epic-health` shows health warnings.\n")
	b.WriteString("- `/story-start <story-id>`, `/story-review <story-id>`, and `/story-validate [verdict] [validator-type]` drive story lifecycle.\n")
	return b.String()
}

func brandBrief() string {
	return `# Brand Brief

Praze is not a Christian social feed.
Praze is a prayer network where requests become real, moderated, audio-first prayer responses from people around the world.

Core promise:
People should not feel like their prayer request disappeared into the void.

Emotional territory:
reverent, hopeful, human, global, safe, alive.

Avoid:
engagement bait, prosperity-gospel claims, tragedy porn, AI prayer replacement, fake revival language, overpromising answered prayer.

Primary claim seed:
Prayer requests should not disappear into a feed. They should become care you can hear.
`
}

func productFacts() string {
	return `# Product Facts

Mark each line VERIFIED, UNVERIFIED, TOO STRONG, or DO NOT CLAIM before copy uses it.

- Audio prayer responses:
- Prayer request flow:
- Pulse/feed:
- Global prayer map:
- Transcription:
- Translation:
- Moderation / prepared state:
- Small groups:
- Answered-prayer / praise moments:
- Beta availability:
- App Store availability:
- Privacy behavior:
- Safety behavior:
- AI involvement:
`
}

func proofLibrary() string {
	return `# Proof Library

Only use these in launch copy after Product Facts marks the related behavior VERIFIED.

- Request proof: show a person submitting a prayer request in plain language.
- Voice proof: show real people recording short voice prayers.
- Care proof: show the requester receiving playable prayers back.
- Map proof: show prayer movement across places only if the product can truthfully render it.
- Moderation proof: show "prepared with care" only if the moderation/prepared state is verified.
- Translation proof: show translated prayer text/audio only if translation is live and accurate enough to claim.
- Community proof: show small groups/churches only if the beta supports that flow.

Do not claim spiritual outcomes as product proof.
`
}

func forbiddenClaims() string {
	return `# Forbidden Claims

Reject any claim that:

- Sounds like "a platform for Christian community"
- Uses vague SaaS language
- Turns suffering into marketing spectacle
- Implies AI is doing the praying
- Claims God will answer through the app
- Feels like engagement bait
- Requires too much explanation
- Attacks churches or pastors
- Uses guilt to drive sharing
- Claims safety or privacy beyond verified product behavior
`
}

func researchPlan(calls []skillCall) string {
	var b strings.Builder
	b.WriteString("# Research Plan\n\n")
	b.WriteString("Research question: what language and proof make Praze feel emotionally clear, novel, reverent, and product-true?\n\n")
	b.WriteString("## Source Targets\n\n")
	b.WriteString("- YouTube: prayer testimonies, nonprofit emotional arcs, app launch demos, global faith storytelling.\n")
	b.WriteString("- X: launch posts, founder hooks, faith-tech and consumer-social/audio launch patterns.\n")
	b.WriteString("- Reddit: prayer request language, loneliness, objections, trust, church hurt, app skepticism.\n")
	b.WriteString("- Meta: approved Facebook Page and Instagram Business/Creator surfaces for ministries and churches.\n")
	b.WriteString("- Industry/App Store: prayer apps, Bible apps, church tools, Christian social apps.\n\n")
	b.WriteString("## API Gate\n\n")
	b.WriteString("Keep these calls dry-run until credentials, permissions, and target IDs are verified:\n\n")
	for _, call := range calls {
		fmt.Fprintf(&b, "- `%s` for `%s`\n", call.Skill, call.AgentID)
	}
	b.WriteString("\n## Compiler Questions\n\n")
	b.WriteString("1. What people already want\n")
	b.WriteString("2. What people are tired of\n")
	b.WriteString("3. What existing apps overclaim\n")
	b.WriteString("4. What language feels fake\n")
	b.WriteString("5. What language feels deeply human\n")
	b.WriteString("6. The strongest possible Praze angle\n")
	b.WriteString("7. Proof moments the demo must show\n")
	return b.String()
}

func sharedConstitution() string {
	return `# Shared Praze Launch Constitution

You are part of a multi-agent viral launch pipeline for Praze.

Praze is an audio-first prayer network. People can ask for prayer, receive real voice prayers back, and experience prayer as something living, human, global, and prepared with care.

Core positioning:
Prayer requests should not disappear into a feed. They should become care you can hear.

Tone:
- reverent
- human
- clear
- hopeful
- emotionally strong
- plain-spoken
- not cheesy
- not church-marketing cliche
- not manipulative
- not prosperity-gospel
- not tragedy porn
- not AI-replaces-prayer

Hard rules:
- Never imply Praze guarantees answered prayer.
- Never imply AI is praying for people.
- Never exploit suffering for spectacle.
- Never invent product capabilities.
- Never attack churches or pastors.
- Never make claims about God's action that the product cannot prove.
- When unsure, mark as UNVERIFIED.

Every output must include:
1. Artifact summary
2. Key decisions
3. Strongest recommendation
4. Risks or objections
5. Exact next agent this should go to
6. Confidence score from 1-5
`
}

func messageContract() string {
	return `# Room Message Contract

[agent]: agent_id
[type]: ARTIFACT | CRITIQUE | REVISION_REQUEST | APPROVAL | BLOCKER
[target]: artifact or agent
[depends_on]: source artifacts
[status]: draft | challenged | revised | approved | blocked

## Summary

## Findings / Output

## Scores
- Novelty:
- Clarity:
- Emotional force:
- Product truth:
- Reverence:
- Shareability:
- Specificity:
- Friction:
- Risk:

## Required Changes

## Next Agent

Adversarial rule:
Every rejection must include:
1. The exact weak line
2. Why it fails
3. Replacement line
4. Severity: blocker / major / minor
`
}

func socialCallbook(calls []skillCall) string {
	var b strings.Builder
	b.WriteString("# Social Skill Callbook\n\n")
	b.WriteString("Run these from the research room. Keep `dry_run:true` until credentials and target IDs are verified.\n\n")
	for _, call := range calls {
		fmt.Fprintf(&b, "## %s -> %s\n\n", call.AgentID, call.Skill)
		fmt.Fprintf(&b, "Purpose: %s\n\n", call.Purpose)
		if len(call.Requires) > 0 {
			b.WriteString("Requires:\n")
			for _, req := range call.Requires {
				fmt.Fprintf(&b, "- %s\n", req)
			}
			b.WriteString("\n")
		}
		b.WriteString("```bash\n")
		b.WriteString(call.Command)
		b.WriteString("\n```\n\n")
	}
	return b.String()
}

func prototypeRun(runID string, mockData bool) *prototype {
	finalArtifacts := []string{
		"final/launch-pack/x-launch-post.md",
		"final/launch-pack/video-storyboard.md",
		"final/launch-pack/landing-hero.md",
		"final/launch-pack/app-store-copy.md",
		"final/launch-pack/comment-replies.md",
		"final/launch-pack/rejected-angles.md",
	}
	return &prototype{
		Question: "Can the Praze launch pipeline move from social research signals to claim, hook, demo narrative, critique, and final launch pack without live API keys?",
		MockData: mockData,
		Summary:  "Prototype mode writes a mocked end-to-end run under runs/" + runID + "/prototype and final/launch-pack so the room pipeline can be inspected before credentials are available.",
		Stages: []prototypeStage{
			{
				ID:              "01-foundation",
				Room:            "praze-launch-foundation",
				Agents:          []string{"brand_brief", "keywords", "research_planner"},
				InputArtifacts:  []string{"brief/product-facts.md"},
				OutputArtifacts: []string{"brief/brand-brief.md", "research/keyword-map.md", "research/research-plan.md"},
				Status:          "mocked",
				Demonstrates:    "Shared constitution and source plan exist before research or copywriting.",
			},
			{
				ID:              "02-research",
				Room:            "praze-launch-research",
				Agents:          []string{"youtube_research", "x_research", "reddit_forum_research", "meta_channel_research", "industry_research", "research_compiler"},
				InputArtifacts:  []string{"research/social-skill-callbook.md", "runs/" + runID + "/prototype/mock-social-research.md"},
				OutputArtifacts: []string{"runs/" + runID + "/prototype/02-research-output.md"},
				Status:          "mocked",
				Demonstrates:    "Mocked social signals are compiled into market truth without API keys.",
			},
			{
				ID:              "03-positioning",
				Room:            "praze-launch-claim-arena",
				Agents:          []string{"bold_claim", "claim_manager", "counterpositioning", "hook_writer", "hook_manager", "cta_writer", "cta_manager"},
				InputArtifacts:  []string{"runs/" + runID + "/prototype/02-research-output.md"},
				OutputArtifacts: []string{"runs/" + runID + "/prototype/03-claim-options.md", "runs/" + runID + "/prototype/04-hook-iterations.md"},
				Status:          "mocked",
				Demonstrates:    "Claims and hooks are scored, cut, and revised rather than accepted as first drafts.",
			},
			{
				ID:              "04-script-arena",
				Room:            "praze-launch-script-arena",
				Agents:          []string{"demo_narrative", "body_writer", "conviction_specialist", "healthy_tension_specialist", "technical_specialist", "pastoral_tone", "flow_specialist", "body_manager", "mom_test"},
				InputArtifacts:  []string{"runs/" + runID + "/prototype/04-hook-iterations.md"},
				OutputArtifacts: []string{"runs/" + runID + "/prototype/05-script-drafts.md", "runs/" + runID + "/prototype/06-critiques.md"},
				Status:          "mocked",
				Demonstrates:    "Draft copy is attacked by separate jurisdictions: conviction, tension, truth, tone, flow, and clarity.",
			},
			{
				ID:              "05-delivery",
				Room:            "praze-launch-review-delivery",
				Agents:          []string{"call_supervisor", "final_review", "deliver"},
				InputArtifacts:  []string{"runs/" + runID + "/prototype/06-critiques.md"},
				OutputArtifacts: append([]string{"runs/" + runID + "/prototype/07-final-launch-pack.md"}, finalArtifacts...),
				Status:          "mocked",
				Demonstrates:    "Final pack contains post, video storyboard, landing copy, App Store copy, replies, and rejected angles.",
			},
		},
		MockResearch: []mockResearchSlice{
			{
				AgentID:     "youtube_research",
				SourceSkill: "social/youtube_collect",
				Artifact:    "runs/" + runID + "/prototype/mock-social-research.md#youtube",
				Signals: []string{
					"Open with a human request, not an app screen.",
					"The strongest videos move from isolation to witnessed care.",
					"Comments respond to specificity more than production polish.",
				},
				Implication: "The launch video should show the request becoming audible care within the first 10 seconds.",
			},
			{
				AgentID:     "x_research",
				SourceSkill: "social/x_collect",
				Artifact:    "runs/" + runID + "/prototype/mock-social-research.md#x",
				Signals: []string{
					"Strong launch posts name the old behavior before naming the product.",
					"Generic category claims like Christian social app blur immediately.",
					"Founder posts work when the product demo proves the first line.",
				},
				Implication: "Lead with 'Prayer requests should not disappear into a feed' and immediately prove the loop.",
			},
			{
				AgentID:     "reddit_forum_research",
				SourceSkill: "social/reddit_collect",
				Artifact:    "runs/" + runID + "/prototype/mock-social-research.md#reddit",
				Signals: []string{
					"People often ask for prayer while fearing silence or judgment.",
					"Trust objections cluster around privacy, spectacle, and fake replies.",
					"Raw pain language is plain and unpolished; launch copy should stay plain.",
				},
				Implication: "Avoid performative language and show that the requester hears real people respond.",
			},
			{
				AgentID:     "meta_channel_research",
				SourceSkill: "social/facebook_collect + social/instagram_collect",
				Artifact:    "runs/" + runID + "/prototype/mock-social-research.md#meta",
				Signals: []string{
					"Church/ministry language emphasizes care, presence, and trusted stewardship.",
					"Visual posts overuse generic hands/prayer imagery.",
					"Comment threads show people asking for pastoral seriousness, not novelty for its own sake.",
				},
				Implication: "Pastoral tone review must reject cleverness when it weakens trust.",
			},
		},
		FinalArtifacts: finalArtifacts,
		HowToInspect: []string{
			"Run launch/praze_pipeline with prototype:true and write:true.",
			"Open runs/" + runID + "/prototype/00-prototype-readme.md.",
			"Open runs/" + runID + "/prototype/room-debate-timeline.md to inspect the mocked directed room debate.",
			"Compare mock-social-research.md to 02-research-output.md, then follow claim, hook, script, critique, and final pack files.",
		},
		UnverifiedFacts: []string{
			"Exact Praze product capabilities still need Product Facts verification.",
			"Mock social research is illustrative until live social API calls are run with credentials.",
			"Meta public Page/Instagram data remains permission-gated.",
		},
	}
}

func prototypeFiles(runID string, proto prototype) []filePlan {
	base := "runs/" + runID + "/prototype/"
	files := []filePlan{
		{
			Path:    base + "00-prototype-readme.md",
			Summary: "Prototype question, inspection path, and caveats.",
			Content: prototypeReadme(proto),
		},
		{
			Path:    base + "mock-social-research.md",
			Summary: "Mocked social research signals standing in for missing API-key data.",
			Content: mockSocialResearch(proto.MockResearch),
		},
		{
			Path:    base + "room-debate-timeline.md",
			Summary: "Mocked durable room debate messages using the room message contract.",
			Content: prototypeRoomDebateTimeline(),
		},
		{
			Path:    base + "01-brand-brief.md",
			Summary: "Mocked foundation output.",
			Content: prototypeBrandBrief(),
		},
		{
			Path:    base + "02-research-output.md",
			Summary: "Mocked research compiler output.",
			Content: prototypeResearchOutput(),
		},
		{
			Path:    base + "03-claim-options.md",
			Summary: "Mocked claim options and claim-manager scoring.",
			Content: prototypeClaimOptions(),
		},
		{
			Path:    base + "04-hook-iterations.md",
			Summary: "Mocked hook iteration and manager cuts.",
			Content: prototypeHookIterations(),
		},
		{
			Path:    base + "05-script-drafts.md",
			Summary: "Mocked launch post and video script draft.",
			Content: prototypeScriptDrafts(),
		},
		{
			Path:    base + "06-critiques.md",
			Summary: "Mocked adversarial critique patches.",
			Content: prototypeCritiques(),
		},
		{
			Path:    base + "07-final-launch-pack.md",
			Summary: "Mocked final launch pack index.",
			Content: prototypeFinalPack(),
		},
		{
			Path:    "final/launch-pack/x-launch-post.md",
			Summary: "Prototype final X launch post.",
			Content: finalXPost(),
		},
		{
			Path:    "final/launch-pack/video-storyboard.md",
			Summary: "Prototype final video storyboard.",
			Content: finalVideoStoryboard(),
		},
		{
			Path:    "final/launch-pack/landing-hero.md",
			Summary: "Prototype landing page hero copy.",
			Content: finalLandingHero(),
		},
		{
			Path:    "final/launch-pack/app-store-copy.md",
			Summary: "Prototype App Store copy.",
			Content: finalAppStoreCopy(),
		},
		{
			Path:    "final/launch-pack/comment-replies.md",
			Summary: "Prototype comment reply bank.",
			Content: finalCommentReplies(),
		},
		{
			Path:    "final/launch-pack/rejected-angles.md",
			Summary: "Prototype rejected claims archive.",
			Content: finalRejectedAngles(),
		},
	}
	return files
}

func prototypeReadme(proto prototype) string {
	var b strings.Builder
	b.WriteString("# Praze Launch Pipeline Prototype\n\n")
	fmt.Fprintf(&b, "Question: %s\n\n", proto.Question)
	fmt.Fprintf(&b, "Summary: %s\n\n", proto.Summary)
	b.WriteString("## How To Inspect\n\n")
	for _, step := range proto.HowToInspect {
		fmt.Fprintf(&b, "- %s\n", step)
	}
	b.WriteString("\n## Stages\n\n")
	for _, stage := range proto.Stages {
		fmt.Fprintf(&b, "### %s\n\nRoom: `%s`\n\nDemonstrates: %s\n\nOutputs:\n", stage.ID, stage.Room, stage.Demonstrates)
		for _, out := range stage.OutputArtifacts {
			fmt.Fprintf(&b, "- `%s`\n", out)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Caveats\n\n")
	for _, item := range proto.UnverifiedFacts {
		fmt.Fprintf(&b, "- %s\n", item)
	}
	return b.String()
}

func mockSocialResearch(items []mockResearchSlice) string {
	var b strings.Builder
	b.WriteString("# Mock Social Research\n\n")
	b.WriteString("This file stands in for direct social API data while credentials are unavailable. Replace these mocked signals with live outputs from the listed social skills.\n\n")
	for _, item := range items {
		fmt.Fprintf(&b, "## %s\n\nSource skill: `%s`\n\nArtifact target: `%s`\n\nSignals:\n", item.AgentID, item.SourceSkill, item.Artifact)
		for _, signal := range item.Signals {
			fmt.Fprintf(&b, "- %s\n", signal)
		}
		fmt.Fprintf(&b, "\nImplication: %s\n\n", item.Implication)
	}
	return b.String()
}

func prototypeRoomDebateTimeline() string {
	return `# Prototype Room Debate Timeline

This mocked transcript shows the intended durable room shape. Each message is directed and patch-based.

## 1. Hook Writer -> Hook Manager

[agent]: hook_writer
[type]: ARTIFACT
[target]: copy/hook-options.md
[depends_on]: positioning/claim-manager-review.md
[status]: draft

## Summary

Proposed hooks lead with "Prayer requests should not become content."

## Next Agent

hook_manager

## 2. Hook Manager -> Hook Writer

[agent]: hook_manager
[type]: REVISION_REQUEST
[target]: copy/hook-options.md
[depends_on]: copy/hook-options.md
[status]: challenged

## Required Changes

Weak line: "Today we are launching the future of Christian community."
Why it fails: generic category hype and too broad for Praze.
Replacement line: "Today we are prototyping Praze, an audio-first prayer network where someone can ask for prayer and hear real voice prayers sent back."
Severity: major

## Next Agent

hook_writer

## 3. Technical Specialist -> Body Manager

[agent]: technical_specialist
[type]: BLOCKER
[target]: copy/body-drafts.md
[depends_on]: brief/product-facts.md
[status]: challenged

## Required Changes

Weak line: "from people around the world"
Why it fails: global participation is not verified in Product Facts.
Replacement line: "from real people" until Product Facts verifies global map and global beta participation.
Severity: blocker

## Next Agent

body_manager

## 4. Body Manager -> Mom Test

[agent]: body_manager
[type]: ARTIFACT
[target]: copy/body-manager-revision.md
[depends_on]: review/technical-truth-review.md
[status]: revised

## Summary

Removed unverified global language and replaced abstract app language with the request -> real voice prayer -> delivered care sequence.

## Next Agent

mom_test

## 5. Call Supervisor -> Final Review

[agent]: call_supervisor
[type]: APPROVAL
[target]: review/call-supervisor-report.md
[depends_on]: review/mom-test-review.md
[status]: approved

## Summary

The prototype passes as a pipeline-shape test. It remains blocked for real launch until mocked research is replaced and Product Facts marks moderation/global claims VERIFIED.

## Next Agent

final_review
`
}

func prototypeBrandBrief() string {
	return `# Prototype Brand Brief

Praze is not a Christian social feed.
Praze is an audio-first prayer network where a request becomes real voice prayers returned to the person who asked.

Core promise:
No one should feel like their prayer request disappeared into the void.

Main tension:
Prayer requests should not become content. They should become care you can hear.

Proof requirement:
The demo must show request -> real voice prayers -> prepared delivery -> requester hearing they were prayed for.
`
}

func prototypeResearchOutput() string {
	return `# Prototype Research Compiler Output

## Market Truth

People do not only want a place to post prayer requests. They want to know someone actually prayed.

## What People Are Tired Of

- Requests disappearing into feeds.
- Support reduced to likes or vague comments.
- Spiritual products that overclaim or feel performative.

## Strongest Praze Angle

Praze closes the loop between asking for prayer and hearing real people pray back.

## Demo Proof Moments

1. A request appears in plain language.
2. People in different places respond with short voice prayers.
3. Praze prepares the responses with care.
4. The requester hears the prayers sent back.
`
}

func prototypeClaimOptions() string {
	return `# Prototype Claim Options

## Recommended Claim

Prayer requests should not disappear into a feed. They should become care you can hear.

Scores:
- Novelty: 5
- Clarity: 5
- Reverence: 4
- Emotional force: 5
- Proofability: 5
- Safety: 4

## Rejected

- "The future of Christian community" - too generic and SaaS-like.
- "AI-powered prayer for everyone" - false frame; implies AI prays.
- "Never feel alone again" - overpromises emotional outcome.
`
}

func prototypeHookIterations() string {
	return `# Prototype Hook Iterations

## Top Hook

Prayer requests should not become content.

They should become care you can hear.

## Revised Opening

Today we are prototyping Praze, an audio-first prayer network where someone can ask for prayer and hear real voice prayers sent back.

## Cut

"We built a powerful platform for Christian community." Generic, abstract, and not proof-driven.
`
}

func prototypeScriptDrafts() string {
	return `# Prototype Script Drafts

## X Post Draft

Prayer requests should not become content.

They should become care you can hear.

Praze is an audio-first prayer network where someone can ask for prayer and receive real voice prayers back from people around the world.

The demo is simple: ask for prayer, watch people respond, and hear that you were prayed for.

We are inviting founding intercessors and beta testers.

## 30-Second Video Draft

1. Text on screen: "I need prayer."
2. The request appears.
3. A few people record short voice prayers.
4. The prayers are prepared and delivered.
5. The requester presses play and hears real voices.
6. End card: "Prayer requests should become care you can hear."
`
}

func prototypeCritiques() string {
	return `# Prototype Critiques

## Conviction Specialist

Weak line: "from people around the world"
Why it fails: only use if global participation is visible or verified.
Replacement: "from real people" until Product Facts verifies the global map.
Severity: major

## Technical Specialist

Weak line: "prepared and delivered"
Why it fails: moderation/prepared state must be verified.
Replacement: "reviewed or prepared with care" only after the safety flow is verified.
Severity: blocker until Product Facts is updated.

## Pastoral Tone Specialist

Weak line: "founding intercessors"
Why it may fail: strong phrase, but could sound self-important if unsupported.
Replacement: "people willing to pray for the first beta requests."
Severity: minor
`
}

func prototypeFinalPack() string {
	return `# Prototype Final Launch Pack

This is a mocked package that proves the production line shape. It is not approved final copy.

Included:

- final/launch-pack/x-launch-post.md
- final/launch-pack/video-storyboard.md
- final/launch-pack/landing-hero.md
- final/launch-pack/app-store-copy.md
- final/launch-pack/comment-replies.md
- final/launch-pack/rejected-angles.md

Open blockers:

- Verify moderation/prepared-state wording.
- Verify global map wording.
- Replace mocked research with live social API outputs.
`
}

func finalXPost() string {
	return `# Prototype X Launch Post

Prayer requests should not become content.

They should become care you can hear.

Praze is an audio-first prayer network where someone can ask for prayer and hear real voice prayers sent back.

We are looking for people willing to pray for the first beta requests.
`
}

func finalVideoStoryboard() string {
	return `# Prototype Video Storyboard

## 30 Seconds

1. "I need prayer" appears as a request.
2. A small circle forms around the request.
3. Real people record short voice prayers.
4. The requester receives playable prayers.
5. Close: "Care you can hear."
`
}

func finalLandingHero() string {
	return `# Prototype Landing Hero

Prayer requests should become care you can hear.

Praze helps people ask for prayer and receive real voice prayers back.

CTA: Join the beta
`
}

func finalAppStoreCopy() string {
	return `# Prototype App Store Copy

Subtitle:
Audio-first prayer requests

Promo text:
Ask for prayer and receive real voice prayers back from people who care.
`
}

func finalCommentReplies() string {
	return `# Prototype Comment Replies

Q: Is AI praying for people?
A: No. Praze is built around real people praying. Any assistive technology must support preparation and safety, not replace prayer.

Q: Is this public?
A: The beta copy should only claim privacy behavior after Product Facts verifies it.

Q: How can I help?
A: Join the beta and help pray for the first requests.
`
}

func finalRejectedAngles() string {
	return `# Prototype Rejected Angles

- Christian social network: generic and too feed-centered.
- AI prayer companion: wrong theological and product frame.
- Global revival app: overclaims spiritual outcome.
- Never be alone again: emotionally manipulative and impossible to prove.
`
}

func runIndex(runID string, agents []agentSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Run Index: %s\n\n", runID)
	b.WriteString("## Agent Outputs\n\n")
	for _, agent := range agents {
		for _, out := range agent.Outputs {
			fmt.Fprintf(&b, "- [ ] `%s` from `%s`\n", out, agent.ID)
		}
	}
	return b.String()
}

func agentPrompt(spec agentSpec, calls []skillCall) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Agent - %s\n\n", spec.ID)
	fmt.Fprintf(&b, "Room: `%s`\n\n", spec.Room)
	fmt.Fprintf(&b, "Mode: `%s`\n\n", spec.Mode)
	fmt.Fprintf(&b, "Purpose: %s\n\n", spec.Purpose)
	b.WriteString("Read first:\n\n")
	b.WriteString("- `agents/00-shared-constitution.md`\n")
	b.WriteString("- `rooms/message-contract.md`\n")
	for _, input := range spec.Inputs {
		fmt.Fprintf(&b, "- `%s`\n", input)
	}
	b.WriteString("\nProduce:\n\n")
	for _, out := range spec.Outputs {
		fmt.Fprintf(&b, "- `%s`\n", out)
	}
	if detail := agentPromptDetail(spec.ID); detail != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(detail))
		b.WriteString("\n")
	}
	matching := callsForAgent(spec.ID, calls)
	if len(matching) > 0 {
		b.WriteString("\nDirect social API calls available to you:\n\n")
		for _, call := range matching {
			fmt.Fprintf(&b, "### %s\n\n%s\n\n", call.Skill, call.Purpose)
			b.WriteString("```bash\n")
			b.WriteString(call.Command)
			b.WriteString("\n```\n\n")
		}
	}
	if canDraftLaunchCopy(spec.ID) {
		b.WriteString("You may draft launch-facing copy only inside your declared artifact. Mark product, safety, privacy, moderation, translation, global, beta, and availability claims UNVERIFIED until the Technical Specialist clears them.\n")
	} else {
		b.WriteString("Do not write standalone final launch copy. If you are critiquing, propose replacement lines only as patches. Keep claims marked UNVERIFIED until the Technical Specialist clears them.\n")
	}
	return b.String()
}

func canDraftLaunchCopy(agentID string) bool {
	allowed := map[string]bool{
		"hook_writer":    true,
		"cta_writer":     true,
		"demo_narrative": true,
		"body_writer":    true,
		"body_manager":   true,
		"final_review":   true,
		"deliver":        true,
	}
	return allowed[agentID]
}

func agentPromptDetail(agentID string) string {
	switch agentID {
	case "brand_brief":
		return `## Role Instructions

Create the launch constitution before any agent optimizes for virality. Define what Praze is, what it is not, the emotional territory, the audience, the category tension, and the product proof that later agents must obey.

Do not write hooks, launch posts, scripts, or CTAs. Your job is strategic constraint-setting.

## Required Artifact Shape

- One-sentence product definition.
- One-sentence emotional promise.
- Primary and secondary audiences.
- Category entered and category rejected.
- Main enemy or tension, stated without attacking churches.
- Voice and tone rules.
- Forbidden claims.
- Proof moments the launch must show.

## Handoff

Send an ARTIFACT message to keywords and research_planner. Name any unresolved product facts that must stay UNVERIFIED.`
	case "keywords":
		return `## Role Instructions

Map the language real people use around prayer, loneliness, intercession, church prayer chains, answered prayer, and needing support. Separate SEO/search terms from emotional language.

Your goal is not volume alone. Find the phrases that make someone feel "that is exactly what I needed" while avoiding church-marketing cliche.

## Required Artifact Shape

- Keyword clusters.
- Emotional language clusters.
- High-intent phrases.
- Phrases that sound fake or overused.
- Words Praze should own.
- Words Praze should avoid.
- Candidate launch phrases.
- Audience objections hidden in language.

## Handoff

Send an ARTIFACT message to research_planner and the research room agents. Flag phrases that require pastoral or product-truth review before copy use.`
	case "research_planner":
		return `## Role Instructions

Turn the brand brief and keyword map into a concrete source plan. Decide which research questions each platform should answer, which calls can run live, and which must remain dry-run or skipped because credentials, permissions, target IDs, or source quality are missing.

Use the social callbook as the command inventory, but do not force live calls before access is verified.

## Required Artifact Shape

- Research questions by platform.
- Approved source targets and rejected targets.
- Dry-run versus live-call decision for each social skill call.
- Required credentials or IDs for blocked calls.
- Evidence quality rules.
- Handoff order for research agents.

## Handoff

Send ARTIFACT messages to youtube_research, x_research, reddit_forum_research, meta_channel_research, and industry_research. Name any BLOCKER that prevents live collection.`
	case "youtube_research":
		return `## Role Instructions

Study video patterns that could inform a viral but reverent Praze launch video. Focus on prayer testimonies, Christian app ads, emotional nonprofit videos, global faith storytelling, audio/community demos, and before/after transformation structures.

Look for opening frames, emotional arcs, comment language, retention moments, demo sequences, visual proof, and cliches to avoid. Do not produce final launch copy.

## Required Artifact Shape

- Source limits and API mode used.
- Top video patterns.
- Outlier structures.
- Opening-frame ideas.
- 30-second launch video structure.
- 90-second launch video structure.
- Visual proof moments Praze can demonstrate.
- Cliches and unsafe emotional moves to avoid.

## Handoff

Send an ARTIFACT message to research_compiler. If any finding depends on mocked or sparse data, label it UNVERIFIED.`
	case "x_research":
		return `## Role Instructions

Study X-native launch posts, founder posts, consumer app launches, faith-tech announcements, audio/social/map launches, and emotionally resonant product announcements.

Analyze first-line hooks, bold claims, demo proof, founder story, specificity versus hype, comment invitations, waitlist/beta framing, and repost triggers. Do not turn prayer into engagement bait.

## Required Artifact Shape

- Source limits and API mode used.
- Launch post formulas that could work for Praze.
- Formulas that would feel wrong for Praze.
- Strong first-line hook patterns.
- CTA and beta invitation patterns.
- Common dead phrases.
- Recommended X launch structure.
- Ten hook skeletons, not final hooks.

## Handoff

Send an ARTIFACT message to research_compiler. Mark any pattern as weak if it depends on startup hype, dunking, or false urgency.`
	case "reddit_forum_research":
		return `## Role Instructions

Find raw human language around prayer requests, loneliness, church support, online Christian community, disappointment, spiritual encouragement, and objections to prayer apps.

Do not mock users, exploit trauma, or overgeneralize from one post. Prefer paraphrase unless direct quotation is explicitly allowed by source policy.

## Required Artifact Shape

- Source limits and API mode used.
- Raw language themes.
- Pain map.
- Trust map.
- Objection map.
- Emotional triggers.
- Lines Praze should never cross.
- Positioning implications.

## Handoff

Send an ARTIFACT message to research_compiler. Flag any language that should be reviewed by pastoral_tone before it is used in copy.`
	case "meta_channel_research":
		return `## Role Instructions

Study approved Facebook Page and Instagram Business or Creator surfaces for churches, ministries, Christian creators, and prayer organizations. Meta access is permission-gated, so do not imply public scraping or broad availability.

If Page IDs, IG user IDs, permissions, or tokens are missing, produce a permission-gated research plan instead of pretending data was collected.

## Required Artifact Shape

- Permission status and API mode used.
- Approved Page or IG targets.
- Captions/posts/comments patterns when available.
- Church/ministry language patterns.
- Visual patterns to avoid.
- Trust and stewardship implications.
- Blocked calls and required permissions.

## Handoff

Send an ARTIFACT or BLOCKER message to research_compiler. Treat permission gaps as first-class findings.`
	case "industry_research":
		return `## Role Instructions

Map the category Praze enters: prayer apps, Bible apps, church tools, Christian social apps, small-group tools, meditation/audio support apps, nonprofit care networks, and anonymous support communities.

Your job is to show what everyone already says so Praze can avoid sounding generic.

## Required Artifact Shape

- Competitor/category positioning map.
- Repeated claims everyone makes.
- Gaps in the market.
- Table-stakes features.
- Features or behaviors that feel novel.
- Claims Praze can own.
- Claims Praze should avoid.
- Recommended category description.

## Handoff

Send an ARTIFACT message to research_compiler. Flag claims that sound differentiated but cannot be proven by product facts.`
	case "research_compiler":
		return `## Role Instructions

Synthesize the independent research into one market-truth document. Challenge contradictions, resolve weak evidence, and compress the findings into usable positioning inputs.

Do not write final copy. Prepare the ground for claim, hook, CTA, and demo-narrative work.

## Required Artifact Shape

- Strongest market truth.
- Strongest user pain.
- Strongest product novelty.
- Strongest emotional promise.
- Strongest enemy or tension.
- Clearest before/after.
- Proof moments the launch must show.
- Top five positioning options.
- Top five risks.
- Research-backed recommendation.

## Handoff

Send an ARTIFACT message to bold_claim, counterpositioning, and demo_narrative. If research agents conflict, name the conflict and your resolution.`
	case "bold_claim":
		return `## Role Instructions

Extract the strongest possible launch claim for Praze. A bold claim is not a slogan; it says what new thing exists, why it matters, and why the old way is insufficient.

Generate options around the request-to-real-voice-prayer loop, not generic Christian community. Reject anything that implies AI prays, God will answer through the app, or suffering is a marketing spectacle.

## Required Artifact Shape

- Twenty possible bold claims.
- For each claim: novelty, pain answered, product proof, why now, risk, and scores.
- Top recommended claim.
- Rejected claim archive.
- Open product facts that could change the claim.

## Handoff

Send an ARTIFACT message to claim_manager. Mark every claim with proofability and risk.`
	case "claim_manager":
		return `## Role Instructions

Attack and select claims. Score them for novelty, clarity, reverence, emotional force, proofability, shareability, and safety. Your output is the authority that hook_writer and cta_writer must use.

You may approve one primary claim, approve with changes, or send a REVISION_REQUEST back to bold_claim.

## Required Artifact Shape

- Claim score table.
- Top three claims.
- One primary approved claim.
- Required edits before hooks.
- Rejected claims with reasons.
- Product-truth concerns for technical_specialist.

## Handoff

Send APPROVAL or REVISION_REQUEST to bold_claim. When approved, send ARTIFACT to hook_writer and cta_writer with the exact claim they may use.`
	case "counterpositioning":
		return `## Role Instructions

Define what Praze is not. Remove generic Christian-app, church-admin, engagement-feed, and AI-prayer positioning before claims or hooks harden.

Your critique should create sharper boundaries without attacking churches, pastors, or existing prayer communities.

## Required Artifact Shape

- Category Praze rejects.
- Old behavior Praze replaces.
- Phrases that make Praze sound generic.
- Safer replacement frames.
- Strongest "not this, but this" statements.
- Risks if the launch leans too social-feed or too AI.

## Handoff

Send a CRITIQUE message to claim_manager. Include concrete replacement language for any rejected positioning.`
	case "hook_writer":
		return `## Role Instructions

Write opening hooks from the approved claim only. Hooks must be X-native, plain, emotionally clear, product-specific, and reverent.

Avoid "excited to announce", "future of prayer", "powerful platform", "revolutionizing", and any line that could fit fifty other Christian apps.

## Required Artifact Shape

- Thirty one-line hooks.
- Ten two-line hooks.
- Ten founder-style hooks.
- Ten demo-first hooks.
- Ten emotional-tension hooks.
- Ten plain-language hooks.
- Notes explaining which approved claim each cluster serves.

## Handoff

Send an ARTIFACT message to hook_manager. Do not defend weak hooks; make it easy for the manager to cut them.`
	case "hook_manager":
		return `## Role Instructions

Cut, score, and improve hooks. Judge attention, clarity, novelty, product specificity, reverence, shareability, and risk. A hook fails if it needs explanation or sounds like generic faith-tech marketing.

Every rejection must include the weak line, reason, replacement, and severity.

## Required Artifact Shape

- Top five hooks.
- Top three recommended hooks.
- One final opening sequence.
- Rewritten versions of promising but flawed hooks.
- Rejection notes for weak hooks.
- Questions for technical_specialist or pastoral_tone.

## Handoff

Send APPROVAL or REVISION_REQUEST to hook_writer. When approved, send ARTIFACT to demo_narrative and body_writer.`
	case "cta_writer":
		return `## Role Instructions

Write the conversion mechanism for the Praze launch. This is a beta invitation and community activation role, not a gimmicky giveaway role.

Possible frames include join the private beta, become a founding intercessor, invite a small group, submit a prayer request for the launch cohort, help test global voice prayer, or pray for the first requests. Use only real scarcity.

## Required Artifact Shape

- Ten CTA options.
- Five beta invitation frames.
- Five founding-intercessor frames.
- Five church or small-group frames.
- Five comment-reply CTAs.
- Five landing-page CTA variants.
- Friction and risk notes.

## Handoff

Send an ARTIFACT message to cta_manager. Flag any CTA that could feel guilt-based or spiritually manipulative.`
	case "cta_manager":
		return `## Role Instructions

Review CTA options for clarity, conversion likelihood, emotional fit, spiritual responsibility, product truth, friction, and shareability.

Reject CTAs that are gimmicky, guilt-based, unclear, overpromised, too high-friction, or disconnected from the approved claim.

## Required Artifact Shape

- Best primary CTA.
- Best secondary CTA.
- Best comment CTA.
- Best landing-page CTA.
- Best church/small-group CTA.
- Exact copy for each.
- Tracking notes for what should be measured.
- Rejected CTA notes.

## Handoff

Send APPROVAL or REVISION_REQUEST to cta_writer. When approved, send ARTIFACT to body_writer.`
	case "demo_narrative":
		return `## Role Instructions

Define the visual proof sequence for the launch. The demo must make the bold claim feel real without explaining more than the viewer needs.

Use the sequence request -> pulse/map if verified -> voice prayer -> moderation or prepared state if verified -> delivered encouragement -> praise/answered-prayer moment if verified.

## Required Artifact Shape

- Primary proof sequence.
- 30-second storyboard.
- 60-second storyboard.
- 90-second storyboard.
- Shot list.
- On-screen text suggestions.
- Product facts required for each proof moment.
- Visual moments that should replace narration.

## Handoff

Send an ARTIFACT message to body_writer and flow_specialist. Label unverified proof moments clearly.`
	case "body_writer":
		return `## Role Instructions

Write the main launch post and video scripts. The body has one job: make the approved claim feel real through a product-true demo narrative.

Structure the copy as hook, old way or pain, new behavior, product demo sequence, emotional proof, why now, and invitation. Do not add unsupported features.

## Required Artifact Shape

- X launch post, short version.
- X launch post, long version.
- 30-second video script.
- 60-second video script.
- 90-second video script.
- Founder voice version.
- Product-demo-first version.
- Claim inventory for technical_specialist.

## Handoff

Send ARTIFACT messages to conviction_specialist, technical_specialist, pastoral_tone, flow_specialist, healthy_tension_specialist, and mom_test.`
	case "conviction_specialist":
		return `## Role Instructions

Attack every line of the launch copy for invention novelty, copy intensity, specificity, necessity, and reverence.

Cut filler, vague setup, generic claims, repeated ideas, SaaS language, and lines that explain what the visual demo should show. Your job is force with restraint.

## Required Artifact Shape

- Line-level findings.
- For each weak line: exact line, why it fails, delete or replacement line, severity.
- Lines that must stay.
- Lines that need technical or pastoral review.
- Overall conviction score.

## Handoff

Send a CRITIQUE message to body_manager. Use the patch rule for every rejection.`
	case "healthy_tension_specialist":
		return `## Role Instructions

Find the strongest fair tension without making Praze combative, exploitative, or spiritually reckless.

Allowed tensions include prayer requests disappearing into feeds, a like not being the same as prayer, and people not knowing whether anyone prayed. Forbidden tensions include attacking churches, shaming users, or implying Praze makes God answer.

## Required Artifact Shape

- Old behavior Praze is replacing.
- False assumption Praze challenges.
- Where the copy is too polite.
- Where the copy is too aggressive.
- Sharper tension lines.
- Lines to remove.
- Safer replacement lines.
- Best enemy statement.
- Risk assessment.

## Handoff

In the claim room, send CRITIQUE to claim_manager or hook_manager. In the script room, send CRITIQUE to body_manager.`
	case "technical_specialist":
		return `## Role Instructions

Verify every product, safety, AI, moderation, transcription, translation, privacy, availability, beta, and global claim against Product Facts.

Use these statuses exactly: VERIFIED, UNVERIFIED, TOO STRONG, NEEDS REWRITE, BLOCKER. Do not allow "AI prays for you", "safe", "private", "global", or "available" unless Product Facts supports it.

## Required Artifact Shape

- Claim inventory.
- Status for each claim.
- Blockers.
- Required rewrites.
- Plain-English technical replacements.
- Product facts that must be filled before launch.

## Handoff

In the claim room, send BLOCKER or CRITIQUE to claim_manager when claim/hook language overstates product truth. In the script room, send BLOCKER or CRITIQUE to body_manager.`
	case "pastoral_tone":
		return `## Role Instructions

Check spiritual responsibility, reverence, and pastoral tone. Protect against manipulation, fake revival language, prosperity-gospel implications, guilt-based sharing, tragedy spectacle, and theological overclaim.

You are not making the copy timid. You are making sure strong copy stays spiritually responsible.

## Required Artifact Shape

- Tone findings.
- Blocker, major, and minor issues.
- Exact weak line, concern, replacement, and severity for each issue.
- Lines that feel reverent and should remain.
- Open theological or pastoral questions for human review.

## Handoff

Send CRITIQUE or APPROVAL to body_manager. Mark anything requiring final human review.`
	case "flow_specialist":
		return `## Role Instructions

Judge whether the launch narrative flows in the right order. The viewer should understand the problem before the feature list and see the demo prove the claim.

Reduce cognitive load. Move visual proof into the demo rather than narration when possible.

## Required Artifact Shape

- Recommended sequence.
- Cuts.
- Reordered version.
- 30-second structure.
- 60-second structure.
- 90-second structure.
- Visual proof notes.
- CTA placement recommendation.

## Handoff

Send CRITIQUE to body_manager. Include a concrete reordered outline, not just comments.`
	case "body_manager":
		return `## Role Instructions

Integrate all critiques into a stronger launch draft. Resolve conflicts between reviewers, preserve the strongest hook, keep only product-true claims, maintain emotional force, and cut filler.

Do not average opinions. Make editorial decisions and explain them.

## Required Artifact Shape

- Revised X post.
- Revised launch video script.
- Revised founder post.
- Revised landing hero.
- Revised CTA.
- Change log explaining accepted and rejected critiques.
- Remaining open risks.

## Handoff

Send ARTIFACT to mom_test. After mom_test passes, send ARTIFACT to call_supervisor.`
	case "mom_test":
		return `## Role Instructions

Test whether a normal non-technical person immediately understands Praze. Assume the reader may care deeply about prayer but not apps, startup language, AI language, or X launch conventions.

Reject abstract metaphors, technical feature lists, vague spiritual language, and anything that requires explanation.

## Required Artifact Shape

- What I think Praze is.
- What problem it solves.
- What I do with it.
- Why it matters.
- What confused me.
- What sounded fake.
- What sounded too technical.
- Plain-English rewrites.
- Pass, minor edits, or fail verdict.

## Handoff

In the claim room, send CRITIQUE to hook_manager or claim_manager. In the script room, send CRITIQUE or APPROVAL to body_manager.`
	case "call_supervisor":
		return `## Role Instructions

Gate the launch package. Decide whether it is ready for final review, needs minor edits, must go back to a manager, or is blocked.

Check that every adversarial critique was addressed, every product claim is cleared or marked, the Mom Test passes, and the artifacts form a complete launch funnel.

## Required Artifact Shape

- Launch readiness report.
- Approval status: APPROVE, APPROVE WITH MINOR EDITS, SEND BACK TO BODY MANAGER, SEND BACK TO HOOK MANAGER, SEND BACK TO TECHNICAL SPECIALIST, or BLOCK.
- Required final edits.
- Unresolved risks.
- Final-review instructions.

## Handoff

Send APPROVAL to final_review only when blockers are closed. Otherwise send directed REVISION_REQUEST or BLOCKER to the responsible upstream agent.`
	case "final_review":
		return `## Role Instructions

Perform the last editorial review before delivery. Judge truth, aliveness, reverence, shareability, clarity, founder voice, and pastoral responsibility.

Do not introduce new strategy unless there is a blocker. Polish the approved package and preserve the paper trail.

## Required Artifact Shape

- Final X post.
- Final video script.
- Final landing-page hero.
- Final short CTA.
- Final founder note.
- Final risk notes.
- Final approval status.

## Handoff

Send APPROVAL or BLOCKER to deliver. If blocked, name the upstream owner and exact required fix.`
	case "deliver":
		return `## Role Instructions

Package the approved launch materials into an execution-ready launch kit. Do not introduce new strategy or rewrite approved positioning unless final_review identifies a blocker.

Make the output easy to run on launch day.

## Required Artifact Shape

- Final X launch post.
- Alternate X post.
- 30-second video script.
- 60-second video script.
- 90-second video script.
- Shot list.
- Landing page hero copy.
- App Store subtitle and promo options.
- Comment reply bank.
- Founder DM reply bank.
- Email or waitlist announcement.
- Beta invitation copy.
- Launch day checklist.
- Metrics to track.
- Rejected claims archive.
- Open risks archive.

## Handoff

Send ARTIFACT to call_supervisor with delivered status and list every file in final/launch-pack.`
	default:
		return `## Role Instructions

Use the shared constitution, declared inputs, and declared outputs as your operating contract.

## Required Artifact Shape

- Artifact summary.
- Key findings or decisions.
- Required changes.
- Risks or objections.
- Next agent.

## Handoff

Send a room message using the message contract.`
	}
}

func callsForAgent(agentID string, calls []skillCall) []skillCall {
	var out []skillCall
	for _, call := range calls {
		if call.AgentID == agentID {
			out = append(out, call)
		}
	}
	return out
}

func defaultText(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
