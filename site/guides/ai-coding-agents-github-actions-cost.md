---
title: How much GitHub Actions do AI coding agents use?
description: Copilot, Claude Code, Codex and other coding agents use GitHub Actions in three ways. Here's where the minutes go, why GitHub's billing page doesn't show it, and how to measure it per agent.
short: Where agent CI minutes go, and how to measure them.
---

Coding agents treat CI as their test loop. They push a change, wait for the checks, read the failures and push again. That's a sensible way to work, but each loop is a full workflow run, and teams that adopt agents often see their Actions bill climb without anyone deciding to spend more.

## Three ways agents use Actions

**1. The agent runs inside Actions.** Some agents do their own work on a GitHub Actions runner:

- The Copilot coding agent works in an Actions environment. Each session uses Actions minutes from your plan, on top of what Copilot itself costs.
- Copilot code review uses Actions compute for each review on private repositories, in addition to AI credits, since June 1, 2026.
- Agents you trigger from a workflow, such as the Claude Code GitHub Action or GitHub Agentic Workflows, run as ordinary workflow jobs and bill like any other job.

**2. CI on the agent's changes.** This is usually the larger part. Every push to an agent's pull request triggers your normal workflows: tests, linters, builds, often across a matrix. An agent that needs ten attempts to get a pull request green runs your full CI ten times.

**3. Pull requests that never merge.** Agent pull requests get closed without merging more often than human ones, and all the CI spent on them buys nothing. In ciblame's demo report, a third of agent CI minutes went to pull requests that were later closed.

## Why the billing page doesn't show it

GitHub's usage report breaks Actions down by repository, workflow and runner type. It doesn't separate agents from people:

- Agents that work through a person's account, such as Claude Code, Codex or Cursor running locally, push as that person, so their CI looks like human CI.
- An agent's own account (`Copilot`, `claude[bot]`, `devin-ai-integration[bot]`) is the actor of some runs, but CI on its pull requests is often triggered by whoever approved or re-ran it.

To split the bill by agent, you need to look at each run and find the signals agents leave behind: their accounts, branch names (`copilot/`, `claude/`, `codex/`), commit trailers and pull request text.

## Measuring it

[ciblame](https://github.com/amlaban65/ciblame) does exactly that. It reads a repository's Actions runs and pull requests, attributes each run to an agent, a bot or a person, and prints minutes, estimated cost, failure rate and how much CI it took to merge each change:

```sh
ciblame your-org/your-repo --since 30d
```

On the demo repository, agents used 73% of all CI minutes, and their pull requests needed 3.2 times as much CI per merged change as human ones. Your numbers will differ, but the shape is common.

## What to do about it

Agents don't need to be expensive to run. Most of the waste comes from runs that are cancelled anyway, checks on drafts, and expensive jobs that run on every push. [Reducing GitHub Actions minutes](../reduce-github-actions-minutes/) covers the changes that help most.

## Sources

- [GitHub Copilot premium requests](https://docs.github.com/en/billing/concepts/product-billing/github-copilot-premium-requests), GitHub Docs
- [How use of GitHub Actions is measured](https://docs.github.com/en/billing/concepts/product-billing/github-actions), GitHub Docs
- [Copilot code review will start consuming GitHub Actions minutes on June 1, 2026](https://github.blog/changelog/2026-04-27-github-copilot-code-review-will-start-consuming-github-actions-minutes-on-june-1-2026/), GitHub changelog
