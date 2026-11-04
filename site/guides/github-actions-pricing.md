---
title: GitHub Actions pricing in 2026: what each runner costs per minute
description: Per-minute prices for GitHub-hosted Linux, Windows and macOS runners in 2026, the minutes each plan includes, what's free, and how GitHub rounds and bills each job.
short: Per-minute prices, included minutes, and what's free.
---

GitHub cut the price of its hosted runners by up to 39% on January 1, 2026. Here's what Actions costs now, and the billing rules that decide how many minutes you actually pay for.

## Standard runners

| Runner | Price per minute |
|---|---|
| Linux, 1 core | $0.002 |
| Linux, 2 cores (`ubuntu-latest`) | $0.006 |
| Windows, 2 cores (`windows-latest`) | $0.010 |
| macOS, 3 or 4 cores (`macos-latest`) | $0.062 |

macOS costs about ten times as much as Linux for the same minute, so one macOS job in a matrix can cost more than the rest of the workflow.

## Larger runners

| Runner | Price per minute |
|---|---|
| Linux, 4 cores | $0.012 |
| Linux, 8 cores | $0.022 |
| Linux, 16 cores | $0.042 |
| Windows, 4 cores | $0.022 |
| Windows, 8 cores | $0.042 |
| macOS, 12 cores (`macos-latest-large`) | $0.077 |
| macOS, M2 Pro (`macos-latest-xlarge`) | $0.102 |
| GPU Linux, 4 cores | $0.052 |

Larger runners are available on GitHub Team and Enterprise Cloud. They're billed even in public repositories, and they can draw from your plan's included usage.

## Minutes included in each plan

| Plan | Minutes per month | Artifact storage |
|---|---|---|
| GitHub Free | 2,000 | 500 MB |
| GitHub Pro | 3,000 | 1 GB |
| GitHub Free for organizations | 2,000 | 500 MB |
| GitHub Team | 3,000 | 2 GB |
| GitHub Enterprise Cloud | 50,000 | 50 GB |


## What's free

- **Public repositories** on standard GitHub-hosted runners.
- **Self-hosted runners.** GitHub announced a $0.002-per-minute platform charge for self-hosted runners starting March 1, 2026, then postponed it to "re-evaluate our approach". As of now it hasn't been applied, and there's no new date.

## How a job's minutes are counted

- **Each job is rounded up to a whole minute.** A workflow with ten jobs that each take 10 seconds bills ten minutes, not two.
- **Every attempt counts.** If a run fails and you re-run it, you pay for both.
- **Waiting doesn't count.** Time queued for a runner isn't billed; time spent running steps, including `sleep` and waiting on services, is.

## A worked example

A pull request workflow with a lint job (40 seconds), a test job (6 minutes 10 seconds) and a build on Linux, Windows and macOS (3 minutes each):

| Job | Billed minutes | Cost |
|---|---|---|
| lint (Linux) | 1 | $0.006 |
| test (Linux) | 7 | $0.042 |
| build (Linux) | 3 | $0.018 |
| build (Windows) | 3 | $0.030 |
| build (macOS) | 3 | $0.186 |
| **Total per run** | **17** | **$0.282** |

An agent that pushes 25 times to get a pull request green runs this workflow 25 times: about $7 for one pull request, two-thirds of it on macOS.

## Sources

- [Actions runner pricing](https://docs.github.com/en/billing/reference/actions-runner-pricing), GitHub Docs
- [Product usage included with each plan](https://docs.github.com/en/billing/reference/product-usage-included), GitHub Docs
- [How use of GitHub Actions is measured](https://docs.github.com/en/billing/concepts/product-billing/github-actions), GitHub Docs
- [Update to GitHub Actions pricing](https://github.blog/changelog/2025-12-16-coming-soon-simpler-pricing-and-a-better-experience-for-github-actions/), GitHub changelog
