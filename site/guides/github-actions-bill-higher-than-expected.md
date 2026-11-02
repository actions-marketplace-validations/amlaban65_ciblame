---
title: Why your GitHub Actions bill is higher than you expected
description: Per-job rounding, re-runs, macOS and Windows prices, larger runners, scheduled workflows and AI agents all add GitHub Actions minutes you might not see in workflow durations. Here's how each one works.
short: The reasons billed minutes add up faster than run times.
---

A workflow that "takes five minutes" can bill far more than five minutes. These are the usual reasons, roughly from most to least common.

## Every job is rounded up separately

GitHub rounds each job up to the next whole minute, then adds them up. A workflow with a 20-job matrix where every job takes 25 seconds runs for under a minute of wall-clock time per job, but bills 20 minutes. The duration shown at the top of a run is wall-clock time, so it doesn't tell you this.

## Re-runs and retries are billed in full

Re-running a failed workflow, or a single failed job, bills the new attempt on top of the old one. Flaky tests that people re-run until they pass quietly double those workflows' cost.

## macOS and Windows cost more per minute

Billing is in dollars, not raw minutes. A minute on a standard macOS runner costs $0.062, about ten times the $0.006 of Linux, and Windows costs $0.010. One macOS job in a matrix can be most of a workflow's cost. See [GitHub Actions pricing](../github-actions-pricing/) for all the rates.

## Larger runners

4-, 8- and 16-core runners cost two to seven times as much per minute as the standard Linux runner. They're also billed in public repositories, where standard runners are free.

## Scheduled workflows

Workflows on a `schedule:` run whether or not anything changed. They're easy to forget because they never show up on a pull request.

## AI coding agents and bots

Agents push to their pull requests many times while they work, and every push runs CI. Some agents, including the Copilot coding agent and Copilot code review, also run on Actions themselves. Bots like Dependabot and Renovate open a pull request per update unless you group them. None of this is labelled on the billing page. [How much Actions AI agents use](../ai-coding-agents-github-actions-cost/) explains how to measure it.

## Storage

Artifacts and caches count against your plan's storage. Beyond that, storage is billed separately from minutes. Large build artifacts kept for the default 90 days can add up.

## See it for your repository

[ciblame](https://github.com/amlaban65/ciblame) adds up billable minutes per job the way GitHub does, and shows which agents, bots, people and workflows they came from:

```sh
ciblame your-org/your-repo --since 30d
```
