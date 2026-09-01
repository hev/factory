# `notify/send` — where the factory's voice goes

Empty in this build, and that is the point: a factory posts flat into one Slack
channel through `scripts/notify.sh`, by incoming webhook or bot token, and a
factory with neither configured says nothing and runs exactly the same.

That is the right shape for one person watching one channel. It stops being the
right shape when a factory runs enough RFCs at once that the channel is three
conversations interleaved — at which point what you want is a thread per RFC,
and a thread is not a formatting choice. An incoming webhook answers `ok` in
plain text: no `ts` comes back, so there is nothing for a later message to reply
into. Threading needs `chat.postMessage`, a bot token, and somewhere to keep the
`ts` it returns.

So this is the seam. Drop an executable here named `send`:

```
notify/send <instance> <from> [thread-key]      message on stdin
```

`scripts/notify.sh` runs it instead of sending, and its exit status is the
script's — non-zero only when a send was attempted and refused, because a status
message is never worth failing a beat over.

**Exit 66 means "not mine".** The script then sends the message itself, exactly
as if nothing were installed. This is how a drop-in that cannot serve a
particular factory — a threading sender on a factory configured with only a
webhook — stays out of the way instead of silencing it. Declining is a normal
outcome and belongs in the drop-in wherever its own configuration is missing;
swallowing the message there is the failure mode this reserves an exit code to
prevent.

**The spool is not yours to skip.** The `posted` event is written before the
exec, by the script, on every message — so the front desk knows what the
operator has been told whether the send worked, failed, or went somewhere this
repo has never heard of. That is deliberately not delegated: every replacement
that had the option to forget eventually did.

`thread-key` names the conversation the message belongs to, normally the Linear
identifier of the RFC it is about (`HEV-31`). It is empty for anything that is
not about one RFC — the WAITING ON YOU block above all, which spans every issue
and belongs in the channel where somebody sees it. A build that cannot thread
ignores the key; the caller never learns which build it is.

Whatever you drop here stays on the machine. `notify/*` is gitignored, this
README excepted, the same way `identity/*` is — see
[`../contracts/extending.md`](../contracts/extending.md) §3.
