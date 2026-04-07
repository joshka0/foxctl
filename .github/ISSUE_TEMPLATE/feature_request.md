---
name: Feature Request
about: Suggest a new feature or enhancement
title: "[Feature] "
labels: ["enhancement", "triage"]
body:
  - type: textarea
    id: problem
    attributes:
      label: Problem / Motivation
      description: What problem does this solve? What use case does it support?
    validations:
      required: true
  - type: textarea
    id: proposal
    attributes:
      label: Proposed solution
      description: How should it work? API surface, CLI flags, etc.
    validations:
      required: true
  - type: textarea
    id: alternatives
    attributes:
      label: Alternatives considered
      description: What other approaches did you consider?
    validations:
      required: false
