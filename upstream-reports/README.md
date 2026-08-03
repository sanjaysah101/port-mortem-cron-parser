# Upstream reports

Three findings from this port, written as ready-to-post GitHub bodies. **None are
filed yet** — they are drafted here so the claims in
[FINDINGS.md](../FINDINGS.md) are actionable rather than just asserted.

| # | file | target | type |
| --- | --- | --- | --- |
| 1 | [01-issue-419-incomplete-diagnosis.md](01-issue-419-incomplete-diagnosis.md) | [issue #419](https://github.com/harrisiirak/cron-parser/issues/419) | comment on open issue |
| 2 | [02-pr-435-review-comment.md](02-pr-435-review-comment.md) | [PR #435](https://github.com/harrisiirak/cron-parser/pull/435) | review comment |
| 3 | [03-new-issue-prev-loop-limit.md](03-new-issue-prev-loop-limit.md) | new issue | **previously unreported bug** |

## Before posting

1. **Re-verify #419 and #435 are still open**, and that #435's formula is
   unchanged. Both were open at upstream commit `8410d37` (2026-08-03). If #435
   merged, report 2 becomes a comment on the *merged* PR or a new issue instead.
2. **Re-run the reproductions** — every snippet in these drafts was executed
   against cron-parser 5.7.0 @ `8410d37` on Node 24.7.0 / tzdata 2025b, but a new
   tzdata release could change a transition date.

## Which one matters most

**Report 3** is the only genuinely new bug: `prev()` throws
`"loop limit exceeded"` on `0 0 0 * * *` in `Pacific/Chatham` — a plain
daily-midnight schedule. Found by differential fuzzing (round 63, seed
1909638832), then minimised by hand. Reports 1 and 2 refine an existing issue and
its open fix.

## Tone

These are written as a contributor reporting findings, not as a critique. Each
one:

- leads with a runnable reproduction
- states the environment and tzdata version, since every claim depends on it
- links the exact upstream source lines
- says what is *uncertain* ("happy to be wrong if there is a path I have missed")

Report 1 corrects the diagnosis in an issue the maintainers already triaged, so it
is deliberately specific about *why* the distinction changes the fix rather than
just saying the existing analysis is incomplete.
