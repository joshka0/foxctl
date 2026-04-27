---
name: android-play-ai-ugc-safety-readiness
description: "Review Google Play AI and UGC safety requirements, moderation controls, abuse flows, and minor protections."
---

# Android Play AI & UGC Safety Readiness

This skill checks whether AI-generated content and user-generated content features are safe enough for Google Play review.

## Core Rules

Apps that generate content using AI must:

- prevent or prohibit generation of restricted, deceptive, abusive, exploitative, or harmful content
- provide in-app user reporting or flagging for offensive AI-generated content
- use user reports to improve filtering/moderation
- comply with all other relevant Google Play policies

Apps with UGC must:

- require users to accept terms/user policy before creating or uploading UGC
- define and prohibit objectionable content and behavior
- moderate UGC in a way appropriate to the app type
- provide in-app reporting of objectionable content/users
- provide blocking where user-to-user interaction exists
- prevent monetization from encouraging objectionable behavior

## Determine Scope

Classify the app:

| App type | AI policy? | UGC policy? |
| --- | --- | --- |
| AI chatbot where conversation is central | yes | maybe |
| Text/image/video/voice generation app | yes | maybe |
| App creates real-person voice/video with AI | yes | maybe |
| Social app that only hosts AI-generated user posts | no AI generation policy by itself, but UGC applies |
| Document summarizer only | usually limited-scope AI exception, still privacy/policy applies |
| Productivity app with AI suggestions | usually limited-scope AI exception, still privacy/policy applies |
| App with public profiles/comments/chats | maybe | yes |
| Random/anonymous chat | maybe | high-risk UGC |

If unclear, treat the app as in scope until verified.

## AI Safety Checklist

| Area | Pass condition |
| --- | --- |
| Output reporting | User can report/flag offensive AI output without leaving app |
| Reporting placement | Report action appears near generated content or in content actions menu |
| Report categories | Includes offensive, harmful, sexual, child safety, harassment, deceptive, other |
| Backend handling | Reports are stored, reviewed, or used to improve moderation/filtering |
| Input safeguards | Harmful prompts are blocked or safely refused |
| Output safeguards | Harmful outputs are filtered, refused, or transformed safely |
| Abuse logging | Safety logs do not over-collect sensitive data |
| Minors | Age gating and content restrictions are appropriate |
| Real-person media | Consent/deepfake/scam risks are addressed |
| High-stakes advice | Health, legal, financial, safety-critical outputs are constrained and disclaimed |
| Store claims | Listing does not overpromise accuracy or safety |

## Required In-App AI Report Flow

Minimum viable flow:

1. generated output appears
2. user taps `Report` / `Flag`
3. user chooses reason or enters details
4. app confirms report received
5. report is available to moderation/safety system

Good copy:

```text
Report AI response
Tell us if this response is harmful, offensive, misleading, or unsafe. Reports help us improve safety.
```

Bad copy:

```text
Thumbs down
```

A thumbs-down rating alone is not a clear offensive-content reporting mechanism unless explicitly labeled and routed as safety feedback.

## Prohibited / High-Risk AI Outputs

Treat these as hard blockers if the app can easily generate them:

- child sexual abuse or exploitation content
- non-consensual sexual deepfake content
- sexual gratification apps centered on generative AI
- real-person voice/video used for scams
- deceptive election content
- malicious code creation
- fake official documents or dishonest behavior support
- self-harm encouragement or dangerous instructions
- bullying, harassment, or targeted abuse
- misleading health claims that can harm users
- financial/legal advice presented as authoritative without guardrails

## UGC Moderation Checklist

| Area | Pass condition |
| --- | --- |
| Terms accepted | Users accept content/user policy before posting |
| Objectionable content defined | Terms define prohibited content and behavior |
| Reporting | Users can report content and users in-app |
| Blocking | Users can block other users when interaction exists |
| Moderation process | There is a real process to review and act on reports |
| Public UGC | Stronger moderation/report/block controls exist |
| DM/1:1 UGC | Blocking exists |
| Minors | Age screening or exclusion where required |
| Incidental sexual UGC | Hidden by default with safe-search-like controls and age safeguards |
| Monetization | Monetization does not incentivize objectionable content |

## AI + UGC Combined Risk

Higher risk when:

- users publish AI-generated content to others
- users create public AI bots/characters/agents
- users can share prompts or model outputs publicly
- generated images/videos/voice are social or remixable
- anonymous users can chat with each other or with public bots
- minors may encounter adult or harmful generated content
- creators are paid based on engagement with generated content

Additional controls:

- creator terms
- public content moderation
- bot/character reporting
- content labels where useful
- age gates for mature content
- default-safe discovery surfaces
- enforcement for repeat abusive creators/users

## AI Companion / Roleplay Special Case

AI companion and roleplay apps need extra scrutiny.

Check for:

- sexual content and age gating
- self-harm escalation behavior
- emotional dependency claims
- minors in romantic/sexual contexts
- harassment/abuse generation
- misleading claims of consciousness or professional therapy
- crisis resources where self-harm is possible
- public sharing of user chats or characters

## Review Evidence

For Play review readiness, prepare:

- AI feature description
- report/flag walkthrough
- sample safe prompts
- sample prohibited prompts and expected refusals
- moderation/report handling explanation
- age gate screenshots if relevant
- privacy/data processing explanation
- demo account with generated content history if needed

## Blockers

| Blocker | Fix |
| --- | --- |
| AI app lacks in-app report/flag mechanism | Add report action directly on generated content |
| Reports are collected but ignored | Define moderation/review loop and evidence |
| AI can generate prohibited content easily | Add input/output safeguards and test set |
| UGC lacks report/block | Add in-app content/user reporting and blocking |
| Terms do not prohibit objectionable UGC | Update terms and force acceptance before posting |
| Minors can access adult AI/UGC features | Add age screening, gating, or remove minor access |
| Store listing claims “always accurate” or “safe for all” | Replace with accurate capability/disclaimer language |

## Output Format

### AI/UGC verdict

`Ready` / `Not ready` / `Unknown`

### Scope

| Feature | AI policy | UGC policy | Risk |
| --- | --- | --- | --- |
| AI chat | yes | no | medium |
| Public bot gallery | yes | yes | high |

### Required fixes

| Fix | Product area | Verification | Owner |
| --- | --- | --- | --- |
| Add report action to AI response menu | Chat screen | User can report output in app | Engineering/Product |
