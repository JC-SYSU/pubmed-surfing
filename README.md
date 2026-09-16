[中文版](README.zh-CN.md) | English

# PubMed Surfing

Biomedical literature work runs on PubMed — search, abstracts, citations, journal screening all live there. PubMed Surfing is a local MCP stdio server that puts that behind eleven well-defined tool calls: structured search, batch abstracts, ID conversion, citation formatting, related-article discovery, journal profiles. It speaks to Codex, Claude Code, and OpenCode over stdin/stdout, needs no account and no API key, and never leaves the machine.

## What it does

One MCP server, eleven tools, three jobs:

1. **Find papers.** `pubmed_search` takes a raw PubMed query, structured fields (title / MeSH / ISSN / year / article type), or PubMed proximity syntax, and returns compact hits with PMIDs, titles, authors, journal, and year. `pubmed_get_total_count` answers "how many papers match" without the hits. `pubmed_find_related` walks ELink neighbor scores from one PMID to surface related work and likely journals.
2. **Read them.** `pubmed_fetch_abstract` and `pubmed_fetch_abstracts_batch` fetch full abstracts and metadata, with section-aware truncation that keeps background/objective and conclusions when PubMed labels sections. When `esummary` times out, search degrades to PMID-only results so the client can still fetch abstracts by ID instead of failing the whole query.
3. **Build output.** `pubmed_convert_ids` switches among DOI / PMID / PMCID via the NCBI PMC ID Converter. `pubmed_format_citations` renders APA, MLA, BibTeX, or RIS from PMIDs or from live results. `pubmed_verify_article_type`, `pubmed_journal_profile`, and `pubmed_journal_mesh_profile` answer journal questions — has this journal published meta-analyses recently, what does its recent article mix look like, what MeSH terms is it about. `pubmed_clear_cache` resets the in-process request cache.

## Sized for AI context

Response sizes are a product decision, not an NCBI accident. By default a search returns at most 10 hits (hard cap 100); raise the cap to PubMed's 10,000 by retrying with `confirm_full: true`. Transport has its own independent layer: up to 200 PMIDs ride one GET request; larger fetches switch to a form-encoded POST in 500-PMID batches, per NCBI's ESummary guidance. One layer bounds what lands in the model's context, the other bounds what one HTTP request carries. Both are capped small on purpose, and `truncated: true` tells you when a result set was cut.

## Who it's for

Anyone whose client does biomedical literature work — reviews, reference formatting, journal screening, checking what is known about a specific topic. The tools run only when the client calls them: nothing watches your session, nothing triggers on keywords, no background polling. Runs anywhere Go runs: macOS, Linux, Windows. Nothing to host, nothing to maintain, no database.

## How to use

**Install** — copy the block below (copy button on the right) and paste it into your agent (Codex, Claude Code, OpenCode, or any coding agent). The agent reads the install guide and does the work:

```
You are installing the PubMed Surfing MCP server. The source repository is https://github.com/JC-SYSU/pubmed-surfing . Do the following:

1. Clone that repository to a sensible location and cd into it. If you cannot clone, read the installation spec directly instead: https://raw.githubusercontent.com/JC-SYSU/pubmed-surfing/main/INSTALL.md
2. Read INSTALL.md in full (the Chinese version is INSTALL.zh-CN.md if preferred) and execute it step by step: probe the machine and pick the install profile (section 0.5), build and verify the source (section 1), release and install (sections 2–3), register the MCP client(s) present on this machine (section 4).
3. Before any step that writes outside this repository (client MCP configuration, PATH, the runtime home) show me what you are about to do and wait for my confirmation.
4. When everything is done, run the acceptance checklist (section 5) and report each item's result to me. Anything that fails: fix it per the troubleshooting table (section 7) before reporting back.
```

Prefer the official release flow: `pubmed-surfingctl release` builds checksummed archives, `install` verifies them (sidecar checksum, path-traversal rejection, platform match, payload hashes) and only switches the active runtime after an MCP smoke test. Details in INSTALL.md.

If you'd rather install by hand, follow `INSTALL.md` directly.

**Day-to-day**: nothing to run, nothing to check. The server is just an MCP command.

## Quick verification

```bash
claude mcp list    # pubmed-surfing present (Claude Code)
codex mcp list     # pubmed-surfing present (Codex)
```

An end-to-end offline check (build → release → install → verify) is in INSTALL.md, "Acceptance checklist".

## License

MIT. Behavioral details — request bounds, cache, partial results, platform coverage — are in INSTALL.md.