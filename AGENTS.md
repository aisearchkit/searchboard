# Agent instructions

Searchboard is developed by multiple AI agents working in parallel, together with human maintainers. Because many people and agents change the code at the same time, every change reaches `main` through a pull request. Nobody commits or pushes to `main` directly.

The full rules are in [docs/ai-guidelines.md](docs/ai-guidelines.md). Read them before changing anything. The essentials:

- Work on a branch created from an up-to-date `main` and open a pull request. Never commit or push to `main`.
- Check open pull requests first so you do not duplicate or collide with another agent's work.
- The PR title must follow `<type>(<scope>): <subject>`; see [docs/contributing.md](docs/contributing.md) and validate it with `scripts/check-pr-title.sh`.
- Write English only: code, comments, docs, commits and PR text.
- Draw diagrams with Mermaid and keep all documentation under `docs/`.
- Run `make check` before opening a pull request.
- When every CI check is green on the latest commit, merge your own pull request yourself with squash and merge (`gh pr merge <number> --squash --delete-branch`). No separate approval is needed.
- Never merge with failing, pending or skipped checks, never use `--admin` or any other bypass, and never weaken a check to get it green.
- Never rewrite, force-push or delete a branch or pull request that another agent or a human owns.
