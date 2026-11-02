#!/usr/bin/env python3
"""Build the ciblame website (GitHub Pages) into site/_site.

    pip install markdown
    python3 site/build.py

Guides are Markdown files in site/guides/ with a small front matter block
(title, description, short). Adding a guide is adding a file.
"""

import datetime
import html
import json
import os
import re
import shutil

import markdown

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.join(HERE, "..")
OUT = os.path.join(HERE, "_site")
SITE = "https://amlaban65.github.io"
BASE = "/ciblame/"
REPO = "https://github.com/amlaban65/ciblame"
TODAY = datetime.date.today().isoformat()
VERSION = "0.1.0"

# One line per row of `ciblame --demo`, shown as git blame output of the CI bill.
# (fake hash, who, kind, minutes, dollars)
BLAME = [
    ("a3f9c1e", "GitHub Copilot", "agent", 9480, 84.33),
    ("4b7d2e0", "humans (5)", "human", 3489, 29.05),
    ("9c1e5a2", "Claude Code", "agent", 3303, 27.22),
    ("e05b7f3", "OpenAI Codex", "agent", 1299, 11.66),
    ("71d4c8b", "scheduled", "bot", 1292, 25.50),
    ("2f8a9d6", "Dependabot", "bot", 881, 7.55),
    ("c6e3b10", "Devin", "agent", 776, 5.32),
    ("58a2f4e", "Copilot review", "agent", 329, 1.97),
]
TOTAL_MIN = 20849

QUICKSTART = (
    "curl -fsSL %s/releases/download/v%s/ciblame_%s_linux_amd64.tar.gz | tar xz ciblame\n"
    "./ciblame --demo                # sample report, no token needed\n"
    "./ciblame your-org/your-repo    # last 30 days of a real repo" % (REPO, VERSION, VERSION)
)
GO = "go run github.com/amlaban65/ciblame/cmd/ciblame@latest --demo"


def read_guide(path):
    with open(path) as f:
        text = f.read()
    m = re.match(r"---\n(.*?)\n---\n", text, re.S)
    meta = {}
    for line in m.group(1).splitlines():
        key, _, value = line.partition(":")
        meta[key.strip()] = value.strip().strip('"')
    meta["slug"] = os.path.splitext(os.path.basename(path))[0]
    meta["body"] = text[m.end():]
    return meta


def esc(s):
    return html.escape(s, quote=True)


def page(title, description, path, body, jsonld):
    url = SITE + BASE + path
    up = "../" * path.count("/")
    return """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title}</title>
<meta name="description" content="{desc}">
<link rel="canonical" href="{url}">
<meta property="og:type" content="{ogtype}">
<meta property="og:title" content="{title}">
<meta property="og:description" content="{desc}">
<meta property="og:url" content="{url}">
<meta property="og:image" content="{site}{base}og.png">
<meta name="twitter:card" content="summary_large_image">
<meta name="color-scheme" content="dark light">
<link rel="icon" href="{up}favicon.svg" type="image/svg+xml">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Martian+Mono:wdth,wght@87.5,400;87.5,600;100,700&family=Source+Sans+3:ital,wght@0,400;0,600;0,700;1,400&display=swap">
<link rel="stylesheet" href="{up}style.css">
<script type="application/ld+json">{jsonld}</script>
</head>
<body>
<header class="bar">
  <a class="brand" href="{up}">ciblame</a>
  <nav aria-label="Site">
    <a href="{up}#guides">Guides</a>
    <a href="{repo}">GitHub</a>
  </nav>
</header>
<main>
{body}
</main>
<footer class="foot">
  <p>ciblame is free and open source under the MIT license. <a href="{repo}">Source on GitHub</a>.
  Know an agent or bot it doesn't recognise? <a href="{repo}/blob/main/CONTRIBUTING.md#adding-or-fixing-an-agent-signature">Adding one</a> is a few lines of JSON.</p>
</footer>
<script src="{up}copy.js" defer></script>
</body>
</html>
""".format(title=esc(title), desc=esc(description), url=url, site=SITE, base=BASE, up=up,
           ogtype="article" if path else "website", jsonld=json.dumps(jsonld), body=body, repo=REPO)


def blame_block():
    rows = []
    top = max(m for _, _, _, m, _ in BLAME)
    for n, (sha, who, kind, minutes, dollars) in enumerate(BLAME, 1):
        rows.append(
            '<tr class="{kind}"><td class="sha">{sha}</td><td class="who"><span class="paren">(</span>{who}</td>'
            '<td class="date">2026-10-{day:02d}</td><td class="ln">{n})</td>'
            '<td class="min">{minutes:,} min</td><td class="usd">${dollars:,.2f}</td>'
            '<td class="share"><span style="--w:{w:.1f}%"></span></td></tr>'.format(
                kind=kind, sha=sha, who=esc(who), day=4 + 3 * n, n=n, minutes=minutes,
                dollars=dollars, w=100.0 * minutes / top))
    return """<figure class="blame">
  <div class="blame-scroll">
    <p class="prompt"><span>$</span> ciblame acme/storefront --since 30d</p>
    <table>
      <caption class="vh">CI minutes and estimated cost per agent, bot and person</caption>
      <tbody>{rows}</tbody>
      <tfoot><tr><td class="sha"></td><td class="who">total</td><td class="date"></td><td class="ln"></td><td class="min">{total:,} min</td><td class="usd">$193</td><td></td></tr></tfoot>
    </table>
  </div>
  <figcaption>A month of CI on a fictional repository, from <code>ciblame --demo</code>.
  <span class="k agent">AI agents</span> used 73% of the minutes,
  <span class="k bot">bots</span> 10% and <span class="k human">people</span> 17%.</figcaption>
</figure>""".format(rows="".join(rows), total=TOTAL_MIN)


def guide_list(guides, skip=None):
    items = []
    for g in guides:
        if g["slug"] == skip:
            continue
        items.append('<li><a href="{href}">{title}</a><span>{short}</span></li>'.format(
            href="%sguides/%s/" % ("../../" if skip else "", g["slug"]),
            title=esc(g["title"]), short=esc(g["short"])))
    return '<ul class="guides">%s</ul>' % "".join(items)


def tool_aside():
    return """<aside class="tool" id="ciblame">
  <h2>See it for your own repositories</h2>
  <p>ciblame reads your Actions runs and pull requests, works out which AI agent, bot or person each run came from,
  and shows the minutes, the estimated cost, how often runs failed, and how much CI it took to merge each change.
  It only reads, and your token never leaves your machine.</p>
  <pre><code>%s</code></pre>
  <p class="small">Or with Go: <code>%s</code>. Binaries for macOS, Windows and Linux are on the <a href="%s/releases">releases page</a>.</p>
</aside>""" % (esc(QUICKSTART), esc(GO), REPO)


def build_index(guides):
    body = """<section class="hero">
  <h1>git blame for your CI bill</h1>
  <p class="lede">ciblame shows how many GitHub Actions minutes and dollars go to AI coding agents like Copilot,
  Claude Code and Codex, to bots like Dependabot, and to the people on your team.</p>
  {blame}
</section>
<section class="start">
  <h2>Try it</h2>
  <p>One binary, no setup. It finds a token in <code>GITHUB_TOKEN</code>, <code>GH_TOKEN</code> or your GitHub CLI login,
  and only reads runs, jobs and pull requests.</p>
  <pre><code>{quick}</code></pre>
  <p class="small">Or with Go 1.23+: <code>{go}</code>. Binaries for macOS, Windows and Linux are on the
  <a href="{repo}/releases">releases page</a>, and a <a href="{repo}#run-it-weekly-in-github-actions">GitHub Action</a>
  posts a report every week.</p>
</section>
<section class="questions">
  <h2>What it answers</h2>
  <ul>
    <li>Which agents are using our CI, and what does each one cost?</li>
    <li>How much CI went to agent pull requests that were never merged?</li>
    <li>How much CI does it take to land one agent change, compared with one human change?</li>
  </ul>
</section>
<section id="guides">
  <h2>Guides</h2>
  <p>How GitHub Actions billing works, and how to spend less of it.</p>
  {guides}
</section>
<section>
  <h2>The full report</h2>
  <img class="shot" src="demo.svg" width="1128" height="568" loading="lazy"
       alt="ciblame terminal report with a row per agent, bot and person: runs, minutes, share, estimated cost, failure rate, merged pull requests and minutes per merge">
</section>""".format(blame=blame_block(), quick=esc(QUICKSTART), go=esc(GO), repo=REPO, guides=guide_list(guides))
    jsonld = {
        "@context": "https://schema.org", "@type": "SoftwareApplication", "name": "ciblame",
        "description": "Shows how many GitHub Actions minutes and dollars go to AI coding agents, bots and people.",
        "applicationCategory": "DeveloperApplication", "operatingSystem": "Linux, macOS, Windows",
        "softwareVersion": VERSION, "url": SITE + BASE, "downloadUrl": REPO + "/releases",
        "license": "https://opensource.org/licenses/MIT",
        "offers": {"@type": "Offer", "price": "0", "priceCurrency": "USD"},
        "author": {"@type": "Person", "name": "Ahmed Laban"},
    }
    return page("ciblame: GitHub Actions cost per AI agent, bot and person",
                "ciblame shows how many GitHub Actions minutes and dollars go to AI coding agents (Copilot, Claude Code, "
                "Codex, Devin), to bots like Dependabot, and to people. Plus guides to Actions pricing and cutting CI minutes.",
                "", body, jsonld)


def build_guide(g, guides):
    md = markdown.Markdown(extensions=["fenced_code", "tables", "toc"])
    body = """<article class="guide">
  <h1>{title}</h1>
  {content}
  {aside}
</article>
<section class="more">
  <h2>More guides</h2>
  {more}
</section>""".format(title=esc(g["title"]), content=md.convert(g["body"]), aside=tool_aside(),
                     more=guide_list(guides, skip=g["slug"]))
    jsonld = {
        "@context": "https://schema.org", "@type": "TechArticle", "headline": g["title"],
        "description": g["description"], "url": "%s%sguides/%s/" % (SITE, BASE, g["slug"]),
        "datePublished": "2026-11-02", "dateModified": TODAY,
        "author": {"@type": "Person", "name": "Ahmed Laban"}, "about": "GitHub Actions billing",
    }
    return page(g["title"], g["description"], "guides/%s/" % g["slug"], body, jsonld)


def write(path, text):
    full = os.path.join(OUT, path)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "w") as f:
        f.write(text)


def main():
    if os.path.exists(OUT):
        shutil.rmtree(OUT)
    gdir = os.path.join(HERE, "guides")
    order = ["ai-coding-agents-github-actions-cost", "github-actions-pricing",
             "github-actions-bill-higher-than-expected", "find-workflows-using-most-github-actions-minutes",
             "reduce-github-actions-minutes"]
    found = {os.path.splitext(n)[0] for n in os.listdir(gdir) if n.endswith(".md")}
    slugs = [s for s in order if s in found] + sorted(found - set(order))
    guides = [read_guide(os.path.join(gdir, s + ".md")) for s in slugs]

    write("index.html", build_index(guides))
    for g in guides:
        write("guides/%s/index.html" % g["slug"], build_guide(g, guides))

    keys = [n for n in os.listdir(HERE) if re.match(r"^[0-9a-f]{32}\.txt$", n)]
    for name in ["style.css", "copy.js", "favicon.svg", "og.png"] + keys:
        src = os.path.join(HERE, name)
        if os.path.exists(src):
            shutil.copy(src, os.path.join(OUT, name))
    shutil.copy(os.path.join(ROOT, "docs", "demo.svg"), os.path.join(OUT, "demo.svg"))

    urls = [SITE + BASE] + ["%s%sguides/%s/" % (SITE, BASE, g["slug"]) for g in guides]
    write("sitemap.xml", '<?xml version="1.0" encoding="UTF-8"?>\n'
          '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n'
          + "".join("  <url><loc>%s</loc><lastmod>%s</lastmod></url>\n" % (u, TODAY) for u in urls)
          + "</urlset>\n")
    write("robots.txt", "User-agent: *\nAllow: /\nSitemap: %s%ssitemap.xml\n" % (SITE, BASE))
    print("built %d pages into %s" % (len(urls), os.path.relpath(OUT)))


if __name__ == "__main__":
    main()
