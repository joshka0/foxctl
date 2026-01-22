# Mobile Companion Architecture

> Design document for building a proactive, autonomous mobile companion on the agentctl framework.

**Status**: Draft
**Author**: Design Session
**Date**: 2026-01-21

---

## Executive Summary

This document outlines the architecture for a mobile companion application that leverages agentctl's multi-agent orchestration to provide:

- **Reactive interactions**: Natural conversation with personality and memory
- **Proactive assistance**: Context-aware suggestions without prompting
- **Autonomous actions**: Purchases, calendar events, reservations (consent-gated)
- **Multi-agent coordination**: Specialized agents for different capabilities

The system uses the Overseer as the central coordinator, with specialized sub-agents for context awareness, planning, payments, and calendar management.

---

## Table of Contents

1. [Goals and Non-Goals](#goals-and-non-goals)
2. [System Architecture](#system-architecture)
3. [Agent Roles](#agent-roles)
4. [Consent and Trust Model](#consent-and-trust-model)
5. [Communication Patterns](#communication-patterns)
6. [Mobile Integration](#mobile-integration)
7. [Expo MVP (Phase 0)](#expo-mvp-phase-0)
8. [Data Model](#data-model)
9. [Skills Required](#skills-required)
10. [Implementation Phases](#implementation-phases)
11. [Open Questions](#open-questions)

---

## Goals and Non-Goals

### Goals (v1)

| Goal | Description |
|------|-------------|
| **Conversational companion** | Natural chat with memory, personality, learned preferences |
| **Multimodal conversation** | Text + images + audio notes with consistent personality |
| **Context awareness** | Location, time, calendar, weather inform suggestions |
| **Proactive planning** | Suggest activities, gifts, reminders without prompting |
| **Autonomous purchases** | x402 micropayments within consent boundaries |
| **Calendar management** | Create events, reminders, detect conflicts |
| **Privacy-first** | All autonomous actions require explicit consent grants |
| **Transparent operation** | User can see what agents are doing and why |

### Non-Goals (v1)

| Non-Goal | Rationale |
|----------|-----------|
| Multi-user/shared companions | Single-user focus for v1 |
| Always-on voice assistant | Defer wake-word + hands-free loop to later |
| Smart home control | Separate integration domain |
| Health/fitness tracking | Privacy and regulatory complexity |
| Financial advice | Liability concerns |

---

## System Architecture

### High-Level Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│                     Mobile App (React Native / Expo)                     │
│                                                                          │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────────────┐ │
│  │ Chat       │  │ Activity   │  │ Approvals  │  │ Settings &         │ │
│  │ Interface  │  │ Feed       │  │ Queue      │  │ Consent Manager    │ │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘  └─────────┬──────────┘ │
└────────┼───────────────┼───────────────┼───────────────────┼────────────┘
         │               │               │                   │
         ▼               ▼               ▼                   ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                        agentctl API Server                               │
│                                                                          │
│  POST /companion/chat          GET /activities                           │
│  POST /companion/context       GET /approvals/pending                    │
│  WS   /ws/companion            POST /approvals/{id}/decide               │
│  GET  /consents                PATCH /consents/{id}                      │
└─────────────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                              Daemon                                      │
│                                                                          │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                         Overseer                                 │    │
│  │                                                                  │    │
│  │  • Agent lifecycle management (spawn, monitor, kill)            │    │
│  │  • Message routing between agents                               │    │
│  │  • Consent policy enforcement                                   │    │
│  │  • Budget tracking and limits                                   │    │
│  │  • Escalation to user when needed                               │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│         │                                                                │
│         ├────────────┬────────────┬────────────┬────────────┐           │
│         ▼            ▼            ▼            ▼            ▼           │
│  ┌───────────┐ ┌───────────┐ ┌───────────┐ ┌───────────┐ ┌───────────┐ │
│  │ Companion │ │ Context   │ │ Planner   │ │ Purchase  │ │ Calendar  │ │
│  │ Agent     │ │ Agent     │ │ Agent     │ │ Agent     │ │ Agent     │ │
│  └───────────┘ └───────────┘ └───────────┘ └───────────┘ └───────────┘ │
│         │            │            │            │            │           │
│         └────────────┴────────────┴────────────┴────────────┘           │
│                                   │                                      │
│                                   ▼                                      │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                        Blackboard                                │    │
│  │                                                                  │    │
│  │  Topics:                                                        │    │
│  │  • user/context      - Current location, time, activity         │    │
│  │  • user/preferences  - Learned likes, dislikes, habits          │    │
│  │  • user/calendar     - Upcoming events, free slots              │    │
│  │  • user/contacts     - Friends, family, relationships           │    │
│  │  • plans/pending     - Suggestions awaiting action              │    │
│  │  • plans/approved    - User-approved plans in progress          │    │
│  │  • consents/active   - Current permission grants                │    │
│  │  • budgets/current   - Spending limits and usage                │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                                                          │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                     Proactive Think Loop                         │    │
│  │                                                                  │    │
│  │  Interval: 5-15 minutes (adaptive based on context)             │    │
│  │  1. Read blackboard for context changes                         │    │
│  │  2. Check consent grants for allowed actions                    │    │
│  │  3. Spawn planner agent if opportunity detected                 │    │
│  │  4. Queue notifications for user attention                      │    │
│  └─────────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────────┘
```

### Deployment Options (On-Device vs Server)

Early design should stay flexible about where the daemon and agents run. The
two endpoints below can also be combined into a hybrid flow.

| Model | Description | Pros | Tradeoffs |
|-------|-------------|------|-----------|
| **On-device daemon** | Agentctl runs locally on the phone; API server is local or minimal. | Lowest latency; stronger privacy boundary; works offline for some features. | OS background limits; battery use; model/runtime size constraints. |
| **Server-hosted daemon** | Mobile app is a thin client; agentctl runs on a server. | Reliable background execution; easier scaling/monitoring; shared compute. | Higher privacy risk; dependency on connectivity; must secure PII. |
| **Hybrid** | Context capture + UI on device; planning/execution on server. | Balances privacy + uptime; can shift work based on consent and cost. | More complex state sync; requires clear data ownership. |

**Note:** Networked skills (search, payments, weather) can be routed through a
server-hosted runner while privacy-sensitive skills (contacts, location) stay
on-device.

### Companion Embeddings (Core, RLM)

The companion treats on-device embeddings as a required capability and builds
context every turn from local semantic recall.

- Embedder: Oplite + Gemma 3, downloaded on first run and cached on device.
- Coverage: embed every user and assistant turn, plus summaries, pins, and
  structured facts.
- Queue: background embedding jobs with content-digest dedup; prefer idle/charging.
- Retrieval: top-K semantic matches + recent N turns + pinned facts.
- Generation: remote LLM (Groq, Cerebras, or hosted) receives curated context only.
- Degraded mode: if the embedder is unavailable, fall back to heuristics
  (recent turns + keyword search) and flag recall as degraded.

### Presence Sessions (On App Open)

When the app opens, create a short-lived "presence session" that boots
lightweight agents and captures realism signals.

- Start Companion Agent and Context Agent on open; keep Planner dormant until
  a trigger is detected.
- Presence signals (consent-gated): time-of-day, coarse location, local
  weather, calendar free/busy, motion/charging state, connectivity, and last
  interaction recency.
- Session TTL: 60-90 seconds for warmup, extend while foregrounded.
- Proactive guardrails: cooldowns, rate limits, and "do not disturb" windows.
- Background: when app is backgrounded, stop active loops and rely on OS
  background tasks or push-only prompts.

### RLM Context Builder (Per Turn)

Each turn builds a compact context bundle from local semantic recall.

1. Collect recent N turns (recency window).
2. Run semantic recall over embedded memory (top-K).
3. Add pinned facts + consented context vars (RLM store).
4. Deduplicate, rank, and trim to token budget.
5. Send curated context to remote LLM for response.

If the embedder is unavailable, fall back to recency + keyword recall and
explicitly mark the response as a degraded-recall turn.

### Embedder Lifecycle Policy

- Download on first run with hash verification and version pinning.
- Cache model in app sandbox; enforce disk budget and LRU eviction.
- Warmup on app open or when device is idle/charging.
- Update in the background with safe rollback to last-good model.
- Gate semantic recall on embedder health; surface degraded mode in UI.

### Presence Session State Machine

```
COLD -> WARM -> ACTIVE -> BACKGROUND -> DORMANT
  |       |       |           |            |
 app open 60-90s  foreground  app bg/idle  stop loops
          warmup  activity    OS tasks     until open
```

- WARM: refresh context vars and run short embedding queue burst.
- ACTIVE: enable proactive checks within consent and cooldown rules.
- BACKGROUND: no active loops; rely on OS background tasks and push.

### Component Responsibilities

| Component | Responsibility |
|-----------|----------------|
| **Mobile App** | UI, push notifications, native APIs (location, calendar) |
| **API Server** | HTTP/WebSocket interface, request routing, auth |
| **Daemon** | Long-running process, agent orchestration, background work |
| **Overseer** | Multi-agent coordination, policy enforcement, escalation |
| **Agents** | Specialized task execution within role boundaries |
| **Blackboard** | Shared state for agent coordination |
| **Mailbox** | Async message passing between agents |

---

## Agent Roles

### Companion Agent (Primary)

The main conversational interface. Always running when user is active.

```yaml
role: companion
exec_mode: reactive
max_iterations: 20

capabilities:
  - Natural conversation with personality
  - Memory recall (preferences, past conversations)
  - Multimodal input handling (images, audio notes)
  - Intent extraction and routing to other agents
  - Synthesizing responses from other agents
  - Emotional awareness and appropriate tone

tools:
  - memory/query
  - memory/put
  - session/recall
  - bb/post        # Post intents to blackboard
  - bb/search      # Read context from blackboard
  - mailbox/send   # Delegate to other agents

personality:
  traits:
    - warm and genuinely interested
    - proactive but not pushy
    - remembers and references past conversations
    - adapts communication style to user preference
```

### Context Agent (Background)

Continuously monitors and updates user context on blackboard.

```yaml
role: context
exec_mode: proactive
think_interval: 60  # seconds

capabilities:
  - Location monitoring (with consent)
  - Time-of-day awareness
  - Weather fetching
  - Calendar sync
  - Contact relationship mapping

tools:
  - mobile/location
  - weather/current
  - calendar/list
  - contacts/list
  - bb/post

outputs_to_blackboard:
  - user/context/location
  - user/context/time_of_day
  - user/context/weather
  - user/context/next_event
  - user/context/nearby_contacts
```

### Planner Agent (On-Demand)

Spawned by overseer when context suggests planning opportunity.

```yaml
role: planner
exec_mode: autonomous
max_auto_turns: 3

capabilities:
  - Activity suggestion based on context
  - Gift idea generation
  - Restaurant/venue recommendations
  - Travel planning
  - Event planning

tools:
  - bb/search           # Read user context and preferences
  - memory/query        # Past preferences, friend info
  - search/places       # Find restaurants, venues
  - search/products     # Find gift ideas
  - bb/post             # Post plans for approval

triggers:
  - Friend birthday approaching (from calendar + contacts)
  - User in new location (from context)
  - Free time detected (from calendar)
  - User mentioned wanting something (from companion)
```

### Purchase Agent (On-Demand, Consent-Required)

Handles x402 micropayments with strict budget controls.

```yaml
role: purchase
exec_mode: autonomous
max_auto_turns: 2

capabilities:
  - x402 payment execution
  - Price comparison
  - Receipt generation
  - Budget tracking

tools:
  - x402/discover       # Find x402-enabled merchants
  - x402/quote          # Get price quotes
  - x402/pay            # Execute payment (consent-gated)
  - bb/search           # Check budget limits
  - bb/post             # Update budget usage

constraints:
  requires_consent: auto_purchase
  max_per_transaction: from_consent
  max_daily: from_consent
  require_approval_above: from_consent
```

### Calendar Agent (On-Demand)

Manages calendar events and scheduling.

```yaml
role: calendar
exec_mode: autonomous
max_auto_turns: 2

capabilities:
  - Event creation
  - Reminder setting
  - Conflict detection
  - Free time finding
  - Recurring event management

tools:
  - calendar/list
  - calendar/create     # consent-gated
  - calendar/update     # consent-gated
  - calendar/delete     # consent-gated
  - bb/post

constraints:
  requires_consent: calendar_management
  can_create: from_consent
  can_modify: from_consent
  can_delete: from_consent
```

---

## Consent and Trust Model

### Philosophy

> "The companion should feel helpful, not creepy. Every autonomous action must be traceable to an explicit user grant."

### Consent Categories

```yaml
consents:
  # --- Data Collection ---
  location_tracking:
    description: "Allow companion to know your location"
    options:
      - always
      - while_using_app
      - never
    default: while_using_app

  calendar_read:
    description: "Allow companion to see your calendar"
    options:
      - full_access
      - free_busy_only
      - never
    default: free_busy_only

  contacts_read:
    description: "Allow companion to know your contacts"
    options:
      - full_access
      - names_only
      - never
    default: names_only

  # --- Proactive Behavior ---
  proactive_suggestions:
    description: "Suggest activities without being asked"
    options:
      - enabled
      - disabled
    default: enabled
    sub_options:
      categories:
        - restaurants
        - entertainment
        - gifts
        - travel
      notify_method:
        - in_app
        - push_notification
        - silent

  # --- Autonomous Actions ---
  auto_purchase:
    description: "Make purchases on your behalf"
    options:
      - enabled
      - disabled
    default: disabled
    when_enabled:
      max_per_transaction:
        type: currency
        default: 25.00
        max: 500.00
      max_daily:
        type: currency
        default: 100.00
        max: 1000.00
      require_confirmation_above:
        type: currency
        default: 10.00
      allowed_categories:
        - gifts
        - food_delivery
        - subscriptions
        - donations

  calendar_write:
    description: "Create or modify calendar events"
    options:
      - create_only
      - create_and_modify
      - full_access
      - disabled
    default: disabled
    when_enabled:
      require_confirmation: true
      allowed_types:
        - reminder
        - tentative_event
```

### Consent Enforcement

```go
// Consent check happens at multiple layers

// 1. Overseer level - before spawning agents
func (o *Overseer) SpawnWithConsent(ctx context.Context, cfg AgentConfig) error {
    required := cfg.Policy.RequiredConsents
    granted := o.consentStore.GetActive(ctx, userID)

    for _, consent := range required {
        if !granted.Has(consent) {
            return ErrConsentRequired{Consent: consent}
        }
    }
    return o.Spawn(ctx, cfg)
}

// 2. Skill level - before executing
func (s *PurchaseSkill) Run(ctx context.Context, input Input) (Output, error) {
    consent := ctx.Value(ConsentKey).(*ConsentGrant)

    if input.Amount > consent.MaxPerTransaction {
        return Output{}, ErrExceedsBudget{
            Requested: input.Amount,
            Limit:     consent.MaxPerTransaction,
        }
    }

    if input.Amount > consent.RequireConfirmationAbove {
        return Output{
            Status:           "pending_approval",
            ApprovalRequired: true,
            ApprovalPrompt:   fmt.Sprintf("Confirm purchase of %s for $%.2f?", input.Item, input.Amount),
        }, nil
    }

    // Execute payment...
}

// 3. Hook level - audit trail
func ConsentAuditHook(event HookEvent) HookOutput {
    if event.Tool == "x402/pay" || event.Tool == "calendar/create" {
        auditLog.Record(AuditEntry{
            Action:    event.Tool,
            Args:      event.Input,
            Consent:   event.Context.ConsentID,
            Timestamp: time.Now(),
        })
    }
    return HookOutput{Decision: Allow}
}
```

### Consent + Evidence (Proposal)

Every autonomous or proactive action should carry an explicit consent and
evidence bundle so approvals are explainable and revocations are immediate.

**Consent token fields (attached to actions):**
- `consent_id`, `consent_type`, `granted_at`, `expires_at`
- `scope` (budget caps, categories, time windows)
- `purpose` (human-readable, e.g., "purchase gift for Alex")
- `source` (where the intent came from: chat thread, proactive loop, etc.)

**Evidence bundle (attached to approvals and audit):**
- `message_refs`: `{thread_id, message_id, ts}` for explicit user intent
- `context_snapshot`: hash or ID for the context used at decision time
- `plan_id` or `proposal_id` tying the action to a reviewed plan

**Revocation semantics:**
- Consent revocation immediately blocks new actions.
- Pending actions tied to a revoked consent are canceled and surfaced to the user.
- Agents must re-request consent if any action exceeds prior scope or purpose.

### Trust Levels

Progressive trust unlocks more autonomous behavior:

| Level | Name | Unlocks | Requirement |
|-------|------|---------|-------------|
| 0 | New User | Chat only | Default |
| 1 | Familiar | Proactive suggestions | 1 week active use |
| 2 | Trusted | Small auto-purchases (<$10) | Explicit consent + 1 month |
| 3 | Partner | Full autonomous actions | Explicit consent + 3 months |

---

## Communication Patterns

### Pattern 1: Reactive Chat

User initiates, companion responds.

```
User: "What should I get Alex for his birthday?"
         │
         ▼
┌─────────────────┐
│ Companion Agent │
│                 │
│ 1. Extract intent: gift_search(friend=Alex, occasion=birthday)
│ 2. Query memory: Alex's preferences, past gifts
│ 3. Send to Planner via mailbox
└────────┬────────┘
         │ agent.ask
         ▼
┌─────────────────┐
│ Planner Agent   │
│                 │
│ 1. Search products matching preferences
│ 2. Filter by user's budget consent
│ 3. Rank by relevance
└────────┬────────┘
         │ agent.reply
         ▼
┌─────────────────┐
│ Companion Agent │
│                 │
│ 1. Format suggestions conversationally
│ 2. Offer to purchase or save for later
└────────┬────────┘
         │
         ▼
User: "I found some ideas for Alex! Based on his love of cooking,
       how about: 1) A chef's knife ($45) 2) Cookbook by...
       Want me to order one, or save these ideas?"
```

### Pattern 2: Proactive Suggestion

System initiates based on context.

```
┌─────────────────┐
│ Context Agent   │ Detects: Friday 5pm, user near downtown, calendar free
└────────┬────────┘
         │ bb/post → user/context/opportunity
         ▼
┌─────────────────┐
│ Proactive Loop  │ Reads blackboard, sees opportunity + consent granted
└────────┬────────┘
         │ spawn
         ▼
┌─────────────────┐
│ Planner Agent   │
│                 │
│ 1. Read context: location, time, weather
│ 2. Read preferences: cuisine likes, budget
│ 3. Search nearby restaurants
│ 4. Post suggestion to plans/pending
└────────┬────────┘
         │ bb/post → plans/pending/dinner_suggestion
         ▼
┌─────────────────┐
│ Notification    │ Push: "It's Friday evening and you're near some
│ Bridge          │        great restaurants. Want suggestions?"
└─────────────────┘
         │
         ▼
User opens app, sees suggestion in Activity Feed
         │
         ▼
User: "Show me the options"
         │
         ▼
┌─────────────────┐
│ Companion Agent │ Retrieves plans/pending/dinner_suggestion
│                 │ Presents options conversationally
└─────────────────┘
```

### Pattern 3: Autonomous Action (with Approval)

Agent acts, but requests confirmation for significant actions.

```
┌─────────────────┐
│ Calendar Agent  │ Detects: Alex's birthday in 3 days (from contacts + calendar)
└────────┬────────┘
         │ agent.ask → Purchase Agent
         ▼
┌─────────────────┐
│ Purchase Agent  │
│                 │
│ 1. Recall: User said "order the chef's knife" last week
│ 2. Find item: $45 (above $10 confirmation threshold)
│ 3. Generate approval request
└────────┬────────┘
         │ escalate
         ▼
┌─────────────────┐
│ Overseer        │ Routes to approval queue
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Notification    │ Push: "Ready to order Alex's gift ($45 chef's knife).
│                 │        Approve to ship by his birthday?"
│                 │        [Approve] [Modify] [Cancel]
└─────────────────┘
         │
         ▼
User taps [Approve]
         │
         ▼
┌─────────────────┐
│ Purchase Agent  │ Executes x402 payment
│                 │ Posts receipt to memory
│                 │ Creates calendar reminder for delivery
└─────────────────┘
```

### Pattern 4: Multi-Agent Coordination

Complex task requiring multiple specialists.

```
User: "Plan a surprise birthday dinner for Alex next Saturday"
         │
         ▼
┌─────────────────┐
│ Companion Agent │ Extracts: plan_event(type=birthday_dinner, for=Alex,
│                 │                       date=next_saturday, surprise=true)
└────────┬────────┘
         │ bb/post → tasks/active/birthday_plan
         │ agent.cmd → Planner: "coordinate birthday plan"
         ▼
┌─────────────────┐
│ Planner Agent   │ Orchestrates sub-tasks
│ (coordinator)   │
└────────┬────────┘
         │
         ├─── agent.ask → Calendar Agent
         │    "Find free evening slots next Saturday for 3 hours"
         │         │
         │         ▼
         │    Returns: [5pm-8pm, 6pm-9pm, 7pm-10pm]
         │
         ├─── agent.ask → Context Agent
         │    "Alex's food preferences and dietary restrictions"
         │         │
         │         ▼
         │    Returns: {likes: ["italian", "sushi"], avoids: ["shellfish"]}
         │
         ├─── agent.ask → Purchase Agent
         │    "Search restaurants: italian OR sushi, no shellfish,
         │     next Saturday 6pm, party of N, reservable"
         │         │
         │         ▼
         │    Returns: [{name: "Trattoria Roma", ...}, ...]
         │
         └─── Synthesizes plan
                   │
                   ▼
         ┌─────────────────┐
         │ Planner Agent   │ Generates complete plan:
         │                 │ - Restaurant options with prices
         │                 │ - Suggested time slot
         │                 │ - Guest list prompt
         │                 │ - Reservation action (pending approval)
         └────────┬────────┘
                  │ bb/post → plans/pending/alex_birthday
                  │ agent.reply → Companion
                  ▼
         ┌─────────────────┐
         │ Companion Agent │ Presents plan to user
         │                 │ "I've put together a plan for Alex's birthday..."
         └─────────────────┘
```

---

## Mobile Integration

### Multimodal Conversation

- Messages can include text, images, and audio notes.
- Store raw attachments locally; embed captions/transcripts for recall.
- Consent-gate camera/microphone access with per-message opt-out.

### Native Capabilities Required

| Capability | iOS | Android | Purpose |
|------------|-----|---------|---------|
| Location | Core Location | Fused Location | Context awareness |
| Calendar | EventKit | CalendarContract | Event management |
| Contacts | Contacts.framework | ContactsContract | Relationship context |
| Camera / Photos | AVFoundation / Photos | CameraX / MediaStore | Multimodal inputs |
| Microphone | AVAudioSession | AudioRecord | Voice notes and audio attachments |
| Push Notifications | APNs | FCM | Proactive engagement |
| Background Refresh | BGTaskScheduler | WorkManager | Context updates |
| Payments | Apple Pay (optional) | Google Pay (optional) | Fallback for x402 |

### React Native Bridge

```typescript
// packages/mobile/src/native/CompanionBridge.ts

interface CompanionBridge {
  // Location
  getCurrentLocation(): Promise<Location>;
  watchLocation(callback: (loc: Location) => void): Subscription;

  // Calendar
  getCalendarEvents(start: Date, end: Date): Promise<CalendarEvent[]>;
  createCalendarEvent(event: CalendarEventInput): Promise<string>;

  // Contacts
  getContacts(): Promise<Contact[]>;
  getContactDetails(id: string): Promise<ContactDetails>;

  // Notifications
  requestNotificationPermission(): Promise<boolean>;
  scheduleLocalNotification(notification: LocalNotification): Promise<string>;

  // Background
  registerBackgroundTask(task: BackgroundTask): void;
}

// Expo implementation
import * as Location from 'expo-location';
import * as Calendar from 'expo-calendar';
import * as Contacts from 'expo-contacts';
import * as Notifications from 'expo-notifications';

export const companionBridge: CompanionBridge = {
  async getCurrentLocation() {
    const { status } = await Location.requestForegroundPermissionsAsync();
    if (status !== 'granted') throw new Error('Location permission denied');
    return Location.getCurrentPositionAsync({});
  },
  // ... etc
};
```

### Background Context Sync

```typescript
// packages/mobile/src/background/ContextSync.ts

import * as BackgroundFetch from 'expo-background-fetch';
import * as TaskManager from 'expo-task-manager';

const CONTEXT_SYNC_TASK = 'companion-context-sync';

TaskManager.defineTask(CONTEXT_SYNC_TASK, async () => {
  try {
    // Gather context
    const location = await getCurrentLocationSafe();
    const nextEvent = await getNextCalendarEvent();
    const weather = await fetchWeather(location);

    // Post to daemon
    await api.post('/companion/context', {
      location,
      next_event: nextEvent,
      weather,
      timestamp: new Date().toISOString(),
    });

    return BackgroundFetch.BackgroundFetchResult.NewData;
  } catch (error) {
    return BackgroundFetch.BackgroundFetchResult.Failed;
  }
});

export async function registerContextSync() {
  await BackgroundFetch.registerTaskAsync(CONTEXT_SYNC_TASK, {
    minimumInterval: 15 * 60, // 15 minutes
    stopOnTerminate: false,
    startOnBoot: true,
  });
}
```

### Push Notification Handling

```typescript
// packages/mobile/src/notifications/Handler.ts

interface CompanionNotification {
  type: 'suggestion' | 'approval_required' | 'action_complete' | 'chat';
  title: string;
  body: string;
  data: {
    thread_id?: string;
    approval_id?: string;
    plan_id?: string;
    action?: 'OPEN_CHAT' | 'SHOW_APPROVAL' | 'SHOW_ACTIVITY';
  };
}

Notifications.addNotificationResponseReceivedListener((response) => {
  const data = response.notification.request.content.data as CompanionNotification['data'];

  switch (data.action) {
    case 'OPEN_CHAT':
      navigation.navigate('Chat', { threadId: data.thread_id });
      break;
    case 'SHOW_APPROVAL':
      navigation.navigate('Approvals', { approvalId: data.approval_id });
      break;
    case 'SHOW_ACTIVITY':
      navigation.navigate('Activity', { planId: data.plan_id });
      break;
  }
});
```

## Expo MVP (Phase 0)

A minimal Expo build that proves on-device recall, multimodal chat, and
presence sessions while delegating generation to a hosted LLM.

**Goals**
- On-device embedder (Oplite + Gemma 3) download on first run.
- Embed every turn; background queue with dedup and idle/charging policy.
- Context builder: semantic top-K + recency + pins -> remote LLM.
- Multimodal composer: text + image + audio notes.
- Image understanding: Gemini 3.0 Flash (server).
- Server STT/TTS (Groq) for audio notes; transcript stored and embedded.
- Presence session warmup on app open (consent-gated signals).
- Custom Expo dev client required for native embedder.

**Screens**
- Onboarding: consent flow + embedder download + storage budget.
- Chat: multimodal composer, recall badge, degraded mode indicator.
- Settings: consents, model status, data export/reset.
- Activity/Approvals: optional for MVP.

**Local data (SQLite)**
- conversations, turns, attachments, embeddings, context_vars, pins, embedding_jobs.
- attachments include: id, turn_id, mime, local_uri, caption, transcript, sha256.

**MVP API surface**
- POST /companion/chat
- POST /companion/context
- POST /speech/stt
- POST /speech/tts (optional)
- POST /vision/describe (Gemini 3.0 Flash)
- GET /consents, PATCH /consents/{id}

**Multimodal flow**
1. User attaches image or audio note.
2. Upload to server for vision/STT.
3. Store derived caption/transcript locally.
4. Embed derived text and add to recall index.
5. Context builder includes semantic matches.

**Guardrails**
- Degraded recall mode when embedder is unhealthy.
- Proactive suggestions behind cooldowns and DND windows.

**Phase 0.5 (optional)**
- Hybrid backfill: server embeddings for backlog if local queue lags.

**App skeleton plan (Expo)**
- app/screens: Onboarding, Chat, Settings, Activity.
- app/components: Composer, AttachmentPreview, RecallBadge.
- app/services: CompanionClient, EmbedderManager, ContextBuilder, PresenceSession,
  VisionService, AudioService.
- app/storage: SQLite schema + repositories for turns/embeddings/context_vars.
- app/queue: EmbeddingQueue worker + dedup.
- app/state: lightweight store for session + model status.

---

## Data Model

### Blackboard Topics

```yaml
# user/context - Current user state (written by Context Agent)
user/context/location:
  lat: 37.7749
  lng: -122.4194
  accuracy: 10
  timestamp: "2026-01-21T18:30:00Z"
  place_name: "Downtown San Francisco"
  place_type: "urban_area"

user/context/time:
  local_time: "18:30"
  day_of_week: "Friday"
  is_weekend: false
  is_holiday: false
  time_of_day: "evening"

user/context/weather:
  temperature_f: 58
  condition: "partly_cloudy"
  precipitation_chance: 10

user/context/calendar:
  next_event:
    title: "Dinner with Sarah"
    start: "2026-01-21T19:30:00Z"
    location: "TBD"
  free_until: "2026-01-21T19:30:00Z"
  busy_today: false

# user/preferences - Learned over time (written by Companion Agent)
user/preferences/food:
  likes: ["italian", "sushi", "thai"]
  dislikes: ["fast_food"]
  dietary: ["no_shellfish"]
  price_range: "moderate"

user/preferences/schedule:
  typical_dinner_time: "19:00"
  prefers_advance_notice: true
  spontaneous_threshold: "same_day"

user/preferences/communication:
  notification_frequency: "moderate"
  prefers_options: true  # vs single recommendation
  detail_level: "concise"

# user/contacts - Relationship context
user/contacts/alex:
  name: "Alex Chen"
  relationship: "close_friend"
  birthday: "2026-01-28"
  interests: ["cooking", "photography", "hiking"]
  gift_history:
    - date: "2025-01-28"
      item: "Camera lens"
      reaction: "loved_it"

# plans/pending - Awaiting user action
plans/pending/dinner_suggestion_01KFG:
  type: "activity_suggestion"
  created_at: "2026-01-21T18:35:00Z"
  created_by: "planner"
  trigger: "context_opportunity"
  content:
    title: "Friday evening dinner"
    options:
      - name: "Trattoria Roma"
        cuisine: "italian"
        price: "$$"
        distance: "0.3mi"
        rating: 4.5
      - name: "Sakura Sushi"
        cuisine: "sushi"
        price: "$$"
        distance: "0.5mi"
        rating: 4.3
  status: "pending"
  expires_at: "2026-01-21T20:00:00Z"

# consents/active - Current permission grants
consents/active/location:
  granted: true
  scope: "while_using_app"
  granted_at: "2026-01-15T10:00:00Z"

consents/active/auto_purchase:
  granted: true
  max_per_transaction: 25.00
  max_daily: 100.00
  require_confirmation_above: 10.00
  daily_used: 0.00
  granted_at: "2026-01-20T14:00:00Z"

# budgets/current - Spending tracking
budgets/current/daily:
  date: "2026-01-21"
  limit: 100.00
  spent: 0.00
  transactions: []

budgets/current/monthly:
  month: "2026-01"
  limit: 500.00
  spent: 127.50
  transactions:
    - date: "2026-01-15"
      amount: 45.00
      item: "Gift for Mom"
    - date: "2026-01-18"
      amount: 82.50
      item: "Concert tickets"
```

### Database Schema Extensions

```sql
-- Consent grants
CREATE TABLE consent_grants (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    consent_type TEXT NOT NULL,  -- 'location', 'auto_purchase', etc.
    granted BOOLEAN NOT NULL DEFAULT FALSE,
    scope TEXT,                   -- JSON: specific parameters
    granted_at TIMESTAMP,
    revoked_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Approval queue
CREATE TABLE approval_requests (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    action_type TEXT NOT NULL,    -- 'purchase', 'calendar_create', etc.
    action_params TEXT NOT NULL,  -- JSON
    amount_cents INTEGER,         -- For purchases
    status TEXT NOT NULL DEFAULT 'pending',  -- 'pending', 'approved', 'rejected', 'expired'
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    decided_at TIMESTAMP,
    expires_at TIMESTAMP
);

-- Audit log
CREATE TABLE companion_audit_log (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    agent_id TEXT,
    action TEXT NOT NULL,
    action_params TEXT,           -- JSON
    consent_id TEXT,
    result TEXT,                  -- 'success', 'failed', 'blocked'
    error_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Activity feed
CREATE TABLE activity_feed (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    type TEXT NOT NULL,           -- 'suggestion', 'action', 'info'
    source_agent TEXT,
    title TEXT NOT NULL,
    body TEXT,
    data TEXT,                    -- JSON: type-specific payload
    read BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

---

## Skills Required

### New Skills to Build

| Skill | Purpose | Priority |
|-------|---------|----------|
| `mobile/location` | Get/watch device location | P0 |
| `mobile/calendar` | Read/write calendar events | P0 |
| `mobile/contacts` | Read contacts and relationships | P1 |
| `mobile/notify` | Send push notifications | P0 |
| `speech/stt` | Speech-to-text for audio notes | P0 |
| `speech/tts` | Text-to-speech for voice playback | P1 |
| `x402/discover` | Find x402-enabled merchants | P1 |
| `x402/quote` | Get payment quotes | P1 |
| `x402/pay` | Execute x402 payment | P1 |
| `search/places` | Search restaurants, venues, etc. | P0 |
| `search/products` | Search gift ideas, items | P1 |
| `weather/current` | Get current weather | P2 |
| `consent/check` | Verify consent for action | P0 |
| `consent/request` | Request new consent from user | P1 |

### Skill: mobile/location

```yaml
apiVersion: agentctl/v1
kind: Skill
metadata:
  name: mobile/location
  version: 0.1.0
  description: "Get device location (requires consent)"
signature:
  command: mobile/location
  parameters:
    - name: action
      type: string
      enum: [current, watch, stop_watch]
      required: true
    - name: accuracy
      type: string
      enum: [coarse, fine]
      default: coarse
returns:
  - name: location
    type: object
    properties:
      lat: number
      lng: number
      accuracy: number
      timestamp: string
capabilities:
  requires_consent: location_tracking
  network: none
  filesystem: none
```

### Skill: x402/pay

```yaml
apiVersion: agentctl/v1
kind: Skill
metadata:
  name: x402/pay
  version: 0.1.0
  description: "Execute x402 micropayment (requires consent + budget)"
signature:
  command: x402/pay
  parameters:
    - name: merchant_url
      type: string
      required: true
    - name: amount_cents
      type: integer
      required: true
    - name: currency
      type: string
      default: "USD"
    - name: memo
      type: string
    - name: require_receipt
      type: boolean
      default: true
returns:
  - name: transaction_id
    type: string
  - name: receipt
    type: object
  - name: status
    type: string
    enum: [success, pending_approval, failed, budget_exceeded]
capabilities:
  requires_consent: auto_purchase
  budget_enforced: true
  network: "https"
  audit_required: true
```

---

## Implementation Phases

### Phase 1: Foundation (2-3 weeks)

**Goal**: Basic chat with context awareness

- [ ] Consent store schema and API
- [ ] Context Agent with location + calendar read
- [ ] Blackboard topics for user context
- [ ] Mobile app: Chat UI + consent settings
- [ ] Push notification infrastructure

**Deliverable**: User can chat, companion knows location and calendar

### Phase 2: Proactive Suggestions (2 weeks)

**Goal**: Companion makes helpful suggestions

- [ ] Proactive think loop in daemon
- [ ] Planner Agent with search/places skill
- [ ] Activity feed UI in mobile app
- [ ] Notification bridge for suggestions

**Deliverable**: Companion suggests restaurants when context is right

### Phase 3: Calendar Integration (2 weeks)

**Goal**: Companion can manage calendar

- [ ] Calendar Agent with create/update
- [ ] Approval flow for calendar changes
- [ ] Conflict detection
- [ ] Reminder creation

**Deliverable**: "Add a reminder for Alex's birthday" works

### Phase 4: Autonomous Purchases (3 weeks)

**Goal**: Companion can buy things (with consent)

- [ ] x402 payment skill integration
- [ ] Budget tracking and enforcement
- [ ] Purchase Agent with approval flow
- [ ] Receipt storage and history

**Deliverable**: "Order that chef's knife for Alex" works

### Phase 5: Multi-Agent Coordination (2 weeks)

**Goal**: Complex tasks spanning multiple agents

- [ ] Planner as coordinator pattern
- [ ] Cross-agent memory sharing
- [ ] Plan synthesis and presentation
- [ ] End-to-end "plan a birthday dinner" flow

**Deliverable**: Complex planning tasks work seamlessly

### Phase 6: Polish and Trust (Ongoing)

**Goal**: Build user trust over time

- [ ] Trust level system
- [ ] Progressive capability unlocking
- [ ] Explanation and transparency features
- [ ] Preference learning improvements

---

## Open Questions

### Product Questions

1. **Notification frequency**: How often is too often for proactive suggestions?
2. **Approval UX**: Inline in chat vs. dedicated approval screen?
3. **Trust building**: How long before allowing auto-purchases?
4. **Failure recovery**: What happens when a purchase fails after approval?

### Technical Questions

1. **Offline mode**: Queue actions when offline, or disable proactive features?
2. **Multi-device**: How to sync state across phone/tablet/watch?
3. **Rate limiting**: How to prevent runaway agent loops?
4. **Cost control**: How to limit LLM costs for proactive thinking?

### Security Questions

1. **Payment security**: How to secure x402 credentials on device?
2. **Consent revocation**: Immediate effect or graceful wind-down?
3. **Audit access**: Can user export full audit log?
4. **Data deletion**: Full companion memory wipe option?

---

## Appendix: Example Conversation

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
                    Friday, 5:47 PM
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

[Push Notification]
🍽️ Friday evening in the city - want some dinner ideas?

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

User opens app

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

🤖 Companion:
I noticed you're downtown and your calendar is free until
your 8pm call. The weather's nice - good evening for a walk
to dinner!

Based on your love of Italian food, here are some spots nearby:

1. **Trattoria Roma** (0.3 mi) ⭐ 4.5
   Cozy spot, great pasta. ~$25-35/person

2. **Cafe Zoetrope** (0.4 mi) ⭐ 4.3
   Coppola's place - good wine selection. ~$30-40/person

3. **Piccolo Forno** (0.2 mi) ⭐ 4.4
   Quick & casual, amazing pizza. ~$15-25/person

Want me to check availability at any of these?

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

👤 User:
Trattoria Roma sounds good. Can you get me a table for 2 at 6:30?

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

🤖 Companion:
Checking Trattoria Roma for 6:30 PM, party of 2...

✅ They have a table! Should I book it?

[Book Table] [Check Other Times] [Never Mind]

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

👤 User taps [Book Table]

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

🤖 Companion:
Done! You're booked at Trattoria Roma:
📅 Tonight at 6:30 PM
👥 Party of 2
📍 123 Columbus Ave (0.3 mi walk)

I've added it to your calendar. Enjoy your dinner! 🍝

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

                    Later that week...

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

[Push Notification]
🎂 Alex's birthday is in 3 days. Ready to order that gift?

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

User opens app

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

🤖 Companion:
Remember last week you said you wanted to get Alex that
chef's knife? His birthday is Tuesday!

I found it in stock:
🔪 **Victorinox Fibrox Pro 8"** - $45.00
⏰ Order by tonight for delivery by Monday

[Order Now - $45] [Show Other Options] [Remind Me Later]

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

👤 User taps [Order Now - $45]

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

🤖 Companion:
This purchase is above your $10 auto-confirm threshold.
Please confirm:

**Victorinox Fibrox Pro Chef's Knife**
$45.00 + free shipping
Delivery: Monday, Jan 27

[Confirm Purchase] [Cancel]

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

👤 User taps [Confirm Purchase]

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

🤖 Companion:
✅ Order confirmed!

📦 **Victorinox Chef's Knife** - $45.00
🚚 Arriving Monday, Jan 27
🎁 For: Alex's birthday

I'll remind you when it arrives. Alex is going to love this!

[View Receipt] [Track Delivery]

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

---

## References

- [Agent Profile v1 Spec](../spec/v1/agent_profile_v1.md)
- [Daemon Protocol](../spec/v1/daemon_protocol.md)
- [Companion Memory Docs](companion-memory.md)
- [x402 Protocol](https://www.x402.org/)
