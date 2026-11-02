---
title: Find which workflows use the most GitHub Actions minutes
description: See which repositories, workflows and jobs use the most GitHub Actions minutes, using GitHub's usage report or ciblame, and what to look for in the results.
short: Repository, workflow and job breakdowns.
---

Before cutting Actions usage, find out where it goes. Usually a few workflows account for most of the minutes, and they're not always the ones you'd guess.

## GitHub's usage report

Go to your organization's or account's **Settings**, then **Billing and licensing**, then **Usage**. Filter by Actions to see minutes and cost over time, grouped by repository. You can also download the usage report, which breaks minutes down further. It's the closest thing to your invoice, but it doesn't separate AI agents and bots from people.

## Per workflow and per job with ciblame

[ciblame](https://github.com/amlaban65/ciblame) reads the runs and jobs of one or more repositories and adds up billable minutes the way GitHub counts them, each job rounded up to a whole minute:

```sh
ciblame your-org/your-repo --by workflow --since 30d
```

To see which agent, bot or person caused the runs instead, leave out `--by workflow`. For your own analysis, export one row per run:

```sh
ciblame your-org/your-repo --format csv --records > runs.csv
```

The token only needs read access to Actions and pull requests.

## What to look for

- **A workflow with many short jobs.** Each job is rounded up to a minute, so fifty 15-second jobs bill fifty minutes. Merging them into fewer jobs can cut minutes a lot.
- **macOS and Windows jobs.** Per minute, macOS costs about ten times as much as Linux and Windows about 1.7 times. A small macOS job can outweigh a large Linux one.
- **High failure rates.** A workflow that fails a third of the time and gets re-run costs a third more than it should. Flaky tests are a cost problem as well as a reliability one.
- **Scheduled workflows.** They run whether or not anything changed, and nobody notices them on pull requests.
- **Pull requests that never merged.** CI spent on abandoned pull requests, often opened by agents or bots, buys nothing.

Once you know which workflows matter, [these changes](../reduce-github-actions-minutes/) are where to start.
