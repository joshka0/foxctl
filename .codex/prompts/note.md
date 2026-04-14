---
description: Add a sticky note to foxctl todo without interrupting current work
argument-hint: <note text>
---
# Sticky Note

Add this note to the foxctl todo list so we don't forget, then continue with what you were doing.

## Note to capture

$ARGUMENTS

## Instructions

1. Use the foxctl todo/manage skill to add a task:
   - Title: `[NOTE] ` followed by a brief summary of the note
   - Description: The full note text, properly escaped for JSON
   - Example: `foxctl run todo/manage --input '{"operation":"add","add":{"title":"[NOTE] Brief summary here","description":"Full note details here"}}'`

2. Briefly confirm the note was added (one short line like "Added note: ...")

3. **Immediately continue with whatever task you were working on before this note** - do not wait for further input, do not ask follow-up questions about the note

This is a non-blocking sticky note. The user wants to capture a thought without losing momentum on their current task.
