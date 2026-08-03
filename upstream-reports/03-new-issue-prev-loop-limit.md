# Report 3 — NEW issue

**Target:** https://github.com/harrisiirak/cron-parser/issues/new
**Type:** new bug report
**Why:** Previously unreported. `prev()` throws on a plain daily-midnight schedule
in `Pacific/Chatham`. Found by differential fuzzing, then minimised by hand.

**Suggested title:**
`prev() throws "loop limit exceeded" on a daily schedule in Pacific/Chatham (spring-forward day)`

---

## Paste from here

### Summary

`prev()` throws `Invalid expression, loop limit exceeded` for a plain
daily-midnight schedule when iterating backward across a spring-forward gap in a
zone with a `:45` base offset. `next()` is unaffected.

### Reproduction

```js
const { CronExpressionParser } = require('cron-parser');

CronExpressionParser.parse('0 0 0 * * *', {
  tz: 'Pacific/Chatham',
  currentDate: '2026-09-27T05:45:00',
}).prev();
```

```
Error: Invalid expression, loop limit exceeded
```

`0 0 0 * * *` is "every day at midnight" — nothing exotic.

### The values are not in dispute

Upstream itself returns the correct answers for the same expression when started
one gap earlier, which shows the schedule is fine and the failure is in reverse
hour-stepping:

```js
// same expression, start 01:00 instead of 05:45
CronExpressionParser.parse('*/5 */11 29-29 */2 0/8', {
  tz: 'Pacific/Chatham', currentDate: '2026-09-27T01:00:00',
}).prev().toISOString();
// -> '2026-09-26T12:10:00.000Z'    (works)

// start 05:45
CronExpressionParser.parse('*/5 */11 29-29 */2 0/8', {
  tz: 'Pacific/Chatham', currentDate: '2026-09-27T05:45:00',
}).prev();
// -> throws
```

### Scope

Needs all three of: a `:45`-offset zone, its spring-forward day, and a target hour
**below** the gap. On 2026-09-27 the local hours are:

```
00:00 +12:45 | 01:00 +12:45 | 02:00 +12:45 | 04:00 +13:45 | 05:00 +13:45
                                            ^^^^^ 03:00 does not exist
```

| expression | `prev()` from `2026-09-27T05:45:00` |
| --- | --- |
| `0 0 0 * * *` | **throws** |
| `0 0 1,11 * * *` | **throws** |
| `0 0 0,11,22 * * *` | **throws** |
| `0 0 22 * * *` | works → `2026-09-26T09:15:00Z` |
| `0 0 11,22 * * *` | works → `2026-09-26T09:15:00Z` |

Unaffected: any other day in Chatham, Chatham's fall-back day, `UTC`,
`America/New_York`.

### A second zone

The same class reproduces in `Australia/Lord_Howe` (30-minute shift) on its
spring-forward day, and there it is expression-dependent — `0 0 0 * * *` succeeds
but this one throws:

```js
CronExpressionParser.parse('18 * 1,29 4/6 1/8', {
  tz: 'Australia/Lord_Howe', currentDate: '2026-10-04T03:15:00',
}).prev();
// -> Error: Invalid expression, loop limit exceeded
```

### Likely area

`#matchHour`'s reverse step-by-step path, taken when `#checkDstTransition` is true
([`CronExpression.ts:471-481`](https://github.com/harrisiirak/cron-parser/blob/master/src/CronExpression.ts#L471-L481)):

```ts
if (this.#checkDstTransition(currentDate)) {
  const steps = reverse ? currentHour - nextHour : nextHour - currentHour;
  for (let i = 0; i < steps; i++) {
    currentDate.applyDateOperation(dateMathVerb, TimeUnit.Hour, hours.length);
    if (!reverse && currentDate.getHours() >= nextHour) break;
    if (reverse && currentDate.getHours() <= nextHour) break;
  }
}
```

`steps` is computed from wall-clock hour numbers, but the gap means the wall clock
does not advance one hour per step. When the target hour is below the gap the
overshoot guards do not fire in the reverse direction and the outer
`#findSchedule` loop re-enters without progress until `LOOP_LIMIT`.

### Related, possibly the same root cause

`#checkDstTransition` returns **false** for zones whose transition is at midnight,
because `startOf('day')` clamps *past* the gap so both samples land on the
post-transition offset:

```js
const { DateTime } = require('luxon');
const d = DateTime.fromISO('2026-09-06T12:00:00', { zone: 'America/Santiago' });
d.startOf('day').offset === d.endOf('day').offset;  // true -> transition not detected
```

So on those zones the fast path in `#matchHour` is taken when it probably should
not be.

### Environment

- cron-parser 5.7.0 @ `8410d3717b7adda1e5b9c5fd6c40cb2cbf9d52e4`
- Node 24.7.0, ICU 77.1, tzdata **2025b**
- Offsets cross-checked identical against tzdata 2025c, so this is not database skew

Found while building a Go port; the differential harness flagged it because the
port returned values where upstream threw. The port deliberately does *not*
reproduce this, and the case is registered with a reproduction here:
https://github.com/sanjaysah101/port-mortem-cron-parser/blob/main/corpus/known-divergences.json
