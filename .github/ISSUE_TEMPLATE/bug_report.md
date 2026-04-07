---
name: Bug Report
about: Report a bug or unexpected behavior
title: "[Bug] "
labels: ["bug", "triage"]
body:
  - type: textarea
    id: description
    attributes:
      label: Description
      description: What happened? What did you expect to happen?
    validations:
      required: true
  - type: textarea
    id: steps
    attributes:
      label: Steps to reproduce
      description: Commands or actions that trigger the issue
      placeholder: |
        1. Run `agentctl ...`
        2. ...
    validations:
      required: true
  - type: textarea
    id: environment
    attributes:
      label: Environment
      description: OS, Go version, agentctl version
      placeholder: |
        - OS: macOS 15 / Ubuntu 24.04
        - Go: 1.25
        - agentctl: v0.X.Y
    validations:
      required: false
  - type: textarea
    id: logs
    attributes:
      label: Relevant logs
      description: Paste any relevant log output (redact secrets!)
      render: shell
    validations:
      required: false
