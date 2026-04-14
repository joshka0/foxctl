---
name: foxctl-companion
description: "Mobile companion skill pack: proactive assistant with context awareness, planning, purchases, and calendar management."
---

## Overview

Skill pack for building a proactive mobile companion that can:
- Chat naturally with personality and memory
- Sense context (location, time, calendar, weather)
- Suggest activities and plan events proactively
- Make purchases (x402) within consent boundaries
- Manage calendar events and reminders

See: `docs/designs/mobile-companion-architecture.md`

## Agent Roles

| Role | Mode | Purpose |
|------|------|---------|
| `companion` | reactive | Primary conversational interface, personality, memory |
| `context` | proactive | Background monitoring, updates blackboard with user state |
| `planner` | autonomous | Activity suggestions, gift ideas, event planning |
| `purchase` | autonomous | x402 payments with budget enforcement |
| `calendar` | autonomous | Event creation, reminders, conflict detection |

## Skills by Category

### Context & Location

| Skill | Status | Purpose |
|-------|--------|---------|
| `mobile/location` | planned | Get/watch device location (consent-gated) |
| `weather/current` | planned | Current weather for location |
| `mobile/calendar` | planned | Read calendar events |
| `mobile/contacts` | planned | Read contacts and relationships |

### Communication

| Skill | Status | Purpose |
|-------|--------|---------|
| `memory/query` | exists | Query past preferences, conversations |
| `memory/put` | exists | Store new memories and preferences |
| `session/recall` | exists | Search past sessions |
| `mobile/notify` | planned | Send push notifications |
| `speech/stt` | planned | Speech-to-text for audio notes |
| `speech/tts` | planned | Text-to-speech for voice playback |

### Planning & Search

| Skill | Status | Purpose |
|-------|--------|---------|
| `search/places` | planned | Search restaurants, venues |
| `search/products` | planned | Search gift ideas, items |
| `codemap/generate` | exists | Generate semantic codemaps |

### Autonomous Actions

| Skill | Status | Purpose |
|-------|--------|---------|
| `x402/discover` | planned | Find x402-enabled merchants |
| `x402/quote` | planned | Get payment quotes |
| `x402/pay` | planned | Execute x402 payment (consent-gated) |
| `mobile/calendar` | planned | Create/update events (consent-gated) |

### Consent & Governance

| Skill | Status | Purpose |
|-------|--------|---------|
| `consent/check` | planned | Verify consent for action |
| `consent/request` | planned | Request new consent from user |
| `mailbox/manage` | exists | Agent-to-agent messaging |
| `bb/post` | exists | Post to blackboard |
| `bb/search` | exists | Search blackboard |

## Blackboard Topics

```yaml
user/context/location     # lat, lng, place_name
user/context/time         # local_time, day_of_week, time_of_day
user/context/weather      # temperature, condition
user/context/calendar     # next_event, free_until
user/preferences/food     # likes, dislikes, dietary
user/preferences/schedule # typical times, spontaneity level
user/contacts/{name}      # relationship, interests, gift_history
plans/pending/{id}        # suggestions awaiting action
consents/active/{type}    # permission grants with scope
budgets/current/daily     # spending limits and usage
```

## Consent Categories

| Category | Scope | Controls |
|----------|-------|----------|
| `location_tracking` | always/while_using/never | Context agent location access |
| `calendar_read` | full/free_busy/never | Calendar visibility |
| `calendar_write` | create/modify/full/none | Calendar mutation |
| `contacts_read` | full/names/never | Contact access |
| `proactive_suggestions` | enabled/disabled | Planner agent triggers |
| `auto_purchase` | enabled/disabled | Purchase agent + budget caps |

## Example Flows

### Reactive: Gift Search
```
User: "What should I get Alex for his birthday?"
         │
         ▼
Companion → memory/query (Alex preferences)
         → mailbox/send → Planner
         │
Planner  → search/products (matching preferences)
         → mailbox/reply
         │
Companion → Format and present options
```

### Proactive: Dinner Suggestion
```
Context Agent → bb/post user/context (Friday 5pm, downtown)
         │
Proactive Loop → detects opportunity + consent
         │
         ▼
Planner  → search/places (nearby restaurants)
         → bb/post plans/pending/dinner
         │
Notification Bridge → mobile/notify "Friday evening ideas?"
```

### Autonomous: Gift Purchase (with approval)
```
Calendar Agent → detects "Alex birthday in 3 days"
         │
Purchase Agent → x402/quote for saved gift
         → amount > threshold → escalate
         │
Overseer → queue approval request
         │
User approves → x402/pay executed
         → receipt stored
         → calendar reminder created
```

## Implementation Priority

### Phase 0 (MVP)
- `mobile/location` - coarse location for context
- `mobile/notify` - push notifications
- `consent/check` - consent enforcement
- `search/places` - restaurant/venue search
- `speech/stt` - audio notes

### Phase 1
- `mobile/calendar` - read + create events
- `mobile/contacts` - relationship context
- `weather/current` - weather awareness

### Phase 2
- `x402/discover`, `x402/quote`, `x402/pay` - payments
- `search/products` - gift/item search
- `consent/request` - dynamic consent prompts

## API Endpoints (MVP)

```yaml
POST /companion/chat           # Main conversation
POST /companion/context        # Update context vars
GET  /companion/context/{id}   # Get context for conversation
WS   /ws/companion             # Real-time updates

GET  /consents                 # List consents
PATCH /consents/{id}           # Update consent

GET  /approvals/pending        # Pending approval queue
POST /approvals/{id}/decide    # Approve/reject action

POST /speech/stt               # Speech-to-text
POST /speech/tts               # Text-to-speech (optional)
POST /vision/describe          # Image understanding
```

## Related Docs

- [Mobile Companion Architecture](../../../docs/designs/mobile-companion-architecture.md)
- [Companion Memory](../../../docs/general/companion-memory.md)
- [Agent Profile v1](../../../docs/spec/v1/agent_profile_v1.md)
