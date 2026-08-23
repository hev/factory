# `identity/<role>` — which account the factory acts as

Empty in this build, and that is the point: a factory runs as **one GitHub
account**, whatever `gh auth` already is. The gaffer dispatches as you, workers
open pull requests as you, reception reads your notifications. Nothing is
provisioned before the first run and no token is written anywhere.

That is the right shape for one person on one machine. It stops being the right
shape when the gaffer should not be you — when a team routes through one desk
and the pull requests need to say who they came from, or when the account doing
the work should be scoped down from the account that owns the repos.

So this is the seam. Drop an executable named after a role here, have it print
a token on stdout, and every `gh` call made in that role uses it:

```
identity/gaffer
identity/reception
identity/worker
```

Nothing else changes. `scripts/lib/gh-auth.sh` calls the hook if it is there
and leaves ambient auth alone if it is not, so the callers never learn which
world they are in.

Shell callers source that directly. Everything else — reception, the gaffer,
and every worker, all of which run `gh` from inside a claude session — is
started through `scripts/factory-as.sh <role> -- <command>`, which resolves the
role and execs, so the session's environment carries the answer. It clears any
inherited token before asking, which is what stops a worker dispatched by the
gaffer from quietly acting as the gaffer.

See [`../contracts/extending.md`](../contracts/extending.md).
