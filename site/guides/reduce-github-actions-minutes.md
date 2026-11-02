---
title: How to reduce GitHub Actions minutes
description: Practical changes that cut GitHub Actions minutes without losing coverage: cancel superseded runs, skip drafts, filter paths, add timeouts, cache dependencies, trim matrices and batch bot pull requests.
short: Changes that cut minutes without losing coverage.
---

Most wasted Actions minutes come from a handful of patterns. Each change below is a few lines of YAML, and none of them reduce what you test before merging.

## Cancel runs that a newer push replaced

When someone, or an agent, pushes twice in a row, the first run is already out of date. Cancel it:

```yaml
concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true
```

This matters most for agents, which often push several times in a few minutes. Don't use it on workflows that deploy, where cancelling half-way can leave things in a bad state.

## Don't run full CI on draft pull requests

Agents often open pull requests as drafts while they work. Run the cheap checks on drafts and the full suite when the pull request is marked ready:

```yaml
on:
  pull_request:
    types: [opened, synchronize, reopened, ready_for_review]

jobs:
  test:
    if: github.event.pull_request.draft == false
```

## Only run what the change can affect

Documentation changes don't need the integration tests:

```yaml
on:
  pull_request:
    paths-ignore:
      - "**.md"
      - "docs/**"
```

If a check is required for merging, skipping it with `paths-ignore` leaves the pull request waiting. In that case, keep the workflow running and skip the expensive jobs inside it instead.

## Set a timeout on every job

A job stuck on a hung test runs until the default limit of 6 hours. Set a realistic limit:

```yaml
jobs:
  test:
    timeout-minutes: 20
```

## Cache dependencies

Downloading and building dependencies on every run is often the slowest part of a job. Most setup actions can cache for you:

```yaml
- uses: actions/setup-node@v4
  with:
    node-version: 22
    cache: npm
```

`actions/setup-python`, `actions/setup-go` and `actions/setup-java` have the same option. For anything else, use `actions/cache`.

## Run the full matrix less often

Testing every OS and language version on each push to a pull request multiplies minutes, and macOS runners cost ten times as much as Linux. Run one combination on pull requests and the full matrix on the default branch or in the merge queue:

```yaml
strategy:
  matrix:
    os: ${{ github.event_name == 'pull_request' && fromJSON('["ubuntu-latest"]') || fromJSON('["ubuntu-latest", "windows-latest", "macos-latest"]') }}
```

## Batch dependency updates

Dependabot opens one pull request per dependency by default, and each one runs your CI. Group them:

```yaml
# .github/dependabot.yml
version: 2
updates:
  - package-ecosystem: npm
    directory: "/"
    schedule:
      interval: weekly
    groups:
      all-dependencies:
        patterns: ["*"]
```

Renovate can do the same with `groupName`, a `schedule`, and `prConcurrentLimit`.

## Check your scheduled workflows

A nightly job that runs for 40 minutes is 1,200 minutes a month, whether or not anyone reads its results. Look at each `schedule:` trigger and ask whether it still needs to run that often.

## Find out where the minutes go first

These changes help most where most of the minutes are. [Find which workflows use the most minutes](../find-workflows-using-most-github-actions-minutes/) before you start, and measure again after.
