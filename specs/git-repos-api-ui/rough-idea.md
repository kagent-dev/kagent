# Rough Idea

Git Repos API + UI

Add a Git Repos feature to kagent — API endpoints and UI pages for managing git repositories that agents can interact with.

Use
https://github.com/reflex-search/reflex
https://github.com/BurntSushi/ripgrep
https://github.com/yoanbernabeu/grepai

```bash
llm install llm-sentence-transformers
llm embed-multi myrepo -m sentence-transformers/all-MiniLM-L6-v2 --files . '**/*.go'
llm similar myrepo -c "where do we set up auth?"
```