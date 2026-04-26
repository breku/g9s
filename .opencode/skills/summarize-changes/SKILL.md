---
name: summarize-changes
description: Summarize uncommitted git changes as a short title and description suitable for a commit message or PR summary
compatibility: opencode
---

## What I do

1. Run `git diff HEAD` to get all uncommitted changes (staged and unstaged).
2. Run `git status --short` to see which files are affected.
3. Produce a structured summary with two parts:
   - **Title**: a single line (≤72 chars), imperative mood, no period. Format: `<type>: <what changed>` where type is one of `feat`, `fix`, `refactor`, `chore`, `docs`, `test`.
   - **Description**: 3–5 bullet points covering *what* changed and *why*, grouped by concern. Each bullet is one concise sentence. No line exceeds 100 chars.

## Output format

Return exactly this structure (no extra prose):

```
Title: <title>

Description:
- <bullet>
- <bullet>
- <bullet>
```

## Rules

- Base everything strictly on the diff — do not invent context.
- If there are no uncommitted changes, say so clearly.
- Do not include file names in the title.
- Bullets should explain intent, not just restate the diff line-by-line.
- It should be a high level overview, not a detailed changelog.
