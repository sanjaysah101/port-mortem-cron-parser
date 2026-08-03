# Report 2 — review comment on PR #435

**Target:** https://github.com/harrisiirak/cron-parser/pull/435
**Type:** review comment on an open PR
**Why:** The proposed fix resolves `Antarctica/Troll` but silently computes zero
compensation for every sub-hour transition, so `Australia/Lord_Howe` stays broken.

**Check before posting:** confirm the PR is still open and the formula unchanged.
It was open as of upstream commit `8410d37` (2026-08-03).

---

## Paste from here

Thanks for tackling this — replacing the wall-clock delta with an offset delta is
the right direction. One case I think this does not cover.

The formula:

```ts
const skippedHours = Math.floor((this.getUTCOffset() - previousOffset) / 60);
```

Evaluated against every transition width that actually exists in tzdata:

| zone | offset delta | `floor(delta / 60)` | adequate? |
| --- | --- | --- | --- |
| `America/New_York` | 60 min | 1 | yes |
| `Antarctica/Troll` | 120 min | 2 | yes |
| **`Australia/Lord_Howe`** | **30 min** | **0** | **no** |
| `Pacific/Chatham` | 60 min | 1 | yes |

`Math.floor(30 / 60)` is `0`. So for a 30-minute spring-forward the fix computes
**zero hours of compensation** for a real 30-minute gap, and a daily 02:00 job
still never runs on 2026-10-04.

Reproduce the underlying facts:

```js
const { DateTime } = require('luxon');
for (const [zone, iso] of [
  ['America/New_York',    '2026-03-08T01:30:00'],
  ['Antarctica/Troll',    '2026-03-29T00:30:00'],
  ['Australia/Lord_Howe', '2026-10-04T01:00:00'],
  ['Pacific/Chatham',     '2026-09-27T02:00:00'],
]) {
  const before = DateTime.fromISO(iso, { zone });
  const after  = before.plus({ hours: 1 }).startOf('hour');
  const delta  = after.offset - before.offset;
  console.log(zone, 'delta', delta, '-> skippedHours', Math.floor(delta / 60));
}
```

```
America/New_York    delta 60  -> skippedHours 1
Antarctica/Troll    delta 120 -> skippedHours 2
Australia/Lord_Howe delta 30  -> skippedHours 0     <-- 30 min skipped, 0 h compensated
Pacific/Chatham     delta 60  -> skippedHours 1
```

So the change converts the bug from *"wrong for widths ≠ 1 h"* to *"wrong for
widths that are not whole hours"* — which still means Lord Howe today, and any
future sub-hour rule.

### The underlying constraint

`dstStart` and `dstEnd` are `number | null` holding **hours**
([`CronDate.ts:25-26`](https://github.com/harrisiirak/cron-parser/blob/master/src/CronDate.ts#L25-L26)),
and `#matchHour` compares them against `currentHour`
([`CronExpression.ts:441`](https://github.com/harrisiirak/cron-parser/blob/master/src/CronExpression.ts#L441)):

```ts
if (currentDate.dstStart !== null && currentDate.dstStart === currentHour - 1) {
```

An hour-granular hint cannot express a half-hour gap regardless of how it is
computed, so I think this needs the compensation carried in **minutes** — which is
a data-model change rather than a one-line formula change. Happy to be wrong if
there is a path I have missed.

Two zones worth adding to the test matrix either way:

- `Australia/Lord_Howe` — 30-minute shift, both directions
- `Pacific/Chatham` — `+12:45` base offset (its *width* is a normal 60 min, so it
  usefully separates "unusual offset" from "unusual width")

Verified against tzdata 2025b (Node 24.7.0 / ICU 77.1); offsets cross-checked
identical against tzdata 2025c.

Full analysis: https://github.com/sanjaysah101/port-mortem-cron-parser/blob/main/FINDINGS.md#1c-the-proposed-fix-pr-435-does-not-fix-1b
