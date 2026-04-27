---
name: apple-ai-privacy-safety-readiness
description: "Review Apple AI, privacy, safety, disclosure, deletion, moderation, and App Review risks for AI or UGC apps."
---

# Apple AI, Privacy, and Safety Readiness

Use this skill to prevent avoidable App Review issues in AI-enabled apps.

## Core Rule

If personal data leaves the device or is shared with a third-party AI service, users must understand that before it happens and must explicitly consent unless another lawful basis clearly applies.

Do not bury AI data sharing only in a privacy policy. Put the meaningful disclosure in the user flow before the data is transmitted.

## Progressive Flow

### Level 0 — AI/privacy verdict

Answer:

- Does the app send personal data to third-party AI?
- Is the disclosure clear before transmission?
- Is explicit consent captured before transmission?
- Do App Privacy answers, privacy policy, and in-app behavior match?
- Are safety/moderation requirements covered?

### Level 1 — Data-flow table

| Data | Source | Sent to | Purpose | User control | Retention |
| --- | --- | --- | --- | --- | --- |
| Prompt text | User input | AI provider | Generate response | Consent before send | X days / none |
| Uploaded file | User picker | AI provider + backend | Summarization | Delete file | X days |
| Chat history | App backend | AI provider | Context | Clear history | X days |

If this table cannot be filled out, the app is not ready.

## Required Checks

### 1. Third-party AI disclosure

Before sending personal data to an AI provider, the app should disclose:

- what data is sent
- who receives it or what category of third party receives it
- why it is sent
- whether it may be stored or used for service improvement
- how the user can avoid or revoke the flow, where applicable

Good copy:

```text
To generate this response, we’ll send your prompt and the selected document to our AI processing provider. Do not include information you do not want processed. You can continue only if you agree.
```

Bad copy:

```text
AI may process data.
```

### 2. Explicit consent

Consent should be:

- opt-in, not implied
- shown before transmission
- specific to AI processing, not bundled invisibly with general terms
- recorded if the app needs auditability
- revocable where ongoing processing is involved

Use clear actions:

- `Continue with AI processing`
- `I agree to send this to AI`
- `Cancel`

Avoid dark patterns:

- prechecked boxes
- consent hidden behind “Next”
- disabling the app when AI processing is not core
- making unrelated features depend on data sharing

### 3. App Privacy answers

Check the App Store privacy questionnaire against reality:

- contact info
- identifiers
- user content
- usage data
- diagnostics
- location
- purchases
- health/fitness
- financial data
- sensitive data

For AI apps, “User Content” is commonly implicated because prompts, uploads, messages, images, audio, and generated content often count as user content.

### 4. Privacy policy

Privacy policy must explain:

- collected data
- collection method
- all uses
- third-party processors / AI processors
- retention
- deletion
- consent withdrawal
- contact method
- children/minors treatment
- international transfer, if relevant

### 5. Account deletion and data deletion

If users can create accounts, the app must provide account deletion in-app.

AI apps should also provide clear controls for:

- deleting chat history
- deleting uploaded documents
- deleting generated projects
- revoking connected services
- disabling memory/personalization, if present

### 6. Minors and Kids Category

If children or teens can use the app:

- avoid unnecessary personal-data collection
- avoid sending minors’ personal data to third-party analytics/ads/AI without strict legal and policy review
- ensure age rating answers match actual AI/UGC behavior
- gate mature content appropriately
- provide moderation and reporting where user content exists

### 7. UGC, creator content, and chat

If the app allows users to publish, share, chat, create public bots, share AI characters, or interact with community content, treat it as UGC/creator risk.

Must include:

- objectionable-content filtering
- report mechanism
- blocking mechanism
- timely response process
- published contact information
- age-rating overflow reporting for creator content
- age restriction for underage users when content exceeds the app rating

Random or anonymous chat is high risk. Do not position it as the primary app experience unless there is a very strong moderation and safety design.

### 8. Misleading AI claims

Avoid unverifiable or deceptive claims:

- “100% accurate AI detector”
- “diagnoses disease” without regulatory support
- “guaranteed legal advice”
- “tracks anyone”
- fake device scanning
- fake surveillance
- fake malware/virus scanning on iOS

AI output should be framed honestly. In high-stakes domains, include qualified limitations and human-review guidance.

### 9. Regulated and high-stakes domains

Escalate for legal/regulatory review when AI touches:

- health or medical advice
- mental health crisis support
- finance, trading, credit, insurance
- legal advice
- education decisions
- employment decisions
- housing decisions
- biometric identification
- children
- gambling, cannabis, crypto, VPN, transportation, aviation

App Review may require documentation, licensing, or proof of authorization.

## Recommended In-App AI Consent Pattern

```tsx
<ConsentSheet
  title="AI processing disclosure"
  body="To answer your request, we’ll send your prompt and selected content to our AI processing provider. Please avoid including sensitive information unless needed."
  primaryAction="Continue with AI processing"
  secondaryAction="Cancel"
  links={[
    { label: 'Privacy Policy', href: privacyUrl },
    { label: 'Manage AI data', href: settingsUrl },
  ]}
/>
```

## App Review Notes Addendum for AI Apps

```text
AI/Data Processing Notes:
- The app uses [AI provider/category] for [feature].
- Before the app sends user-provided content to AI processing, users see a disclosure and must choose “Continue with AI processing.”
- The disclosure is located at: [flow/screen].
- Users can delete AI history at: [settings path].
- The privacy policy describes AI processing and third-party sharing at: [URL/section].
- For review, use the demo prompt/file: [sample].
```

## Output Format

```md
## AI/privacy verdict
Ready / Blocked / Needs manual review

## Blocking issues
| Issue | Guideline risk | Fix | Verification |
| --- | --- | --- | --- |

## Data flow
| Data | Source | Third party | Consent point | Retention |
| --- | --- | --- | --- | --- |

## Copy changes
| Screen | Before | After |
| --- | --- | --- |

## Review notes addendum
[copy-ready text]
```
