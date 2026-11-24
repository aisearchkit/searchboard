# Searchboard Documentation

All project documentation lives in this directory. The repository root keeps only `README.md`, `AGENTS.md` and `CLAUDE.md`, which point here.

| Document | Purpose |
| :--- | :--- |
| [architecture.md](architecture.md) | System design, components and roadmap |
| [development.md](development.md) | Local setup, commands and project conventions |
| [devtools.md](devtools.md) | DevTools console server side: risk rules, confirmation, history |
| [api.md](api.md) | HTTP API reference |
| [esclient.md](esclient.md) | How the adapter hides Elasticsearch and OpenSearch version differences |
| [contributing.md](contributing.md) | Pull request workflow, merge rules, PR title format, language and diagram rules |
| [ai-guidelines.md](ai-guidelines.md) | Rules for AI coding agents: multi-agent collaboration, pull requests and self-merge after CI |

Documentation rules:

- Write in English only.
- Draw every architecture or flow diagram with [Mermaid](https://mermaid.js.org/) inside a Markdown code block.
- Update the relevant document in the same pull request that changes behavior.
