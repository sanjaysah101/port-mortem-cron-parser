# Report 1 — comment on existing issue #419

**Target:** https://github.com/harrisiirak/cron-parser/issues/419
**Type:** comment on an open issue (not a new issue)
**Why:** #419 correctly identifies that `diff === 2` fails for a two-hour
transition, but the 30-minute case fails by a *different* mechanism, and the
distinction changes what a correct fix looks like.

---

## Paste from here

While porting this library I reproduced #419 and found the diagnosis is
incomplete in a way that matters for the fix.

`applyDateOperation` compares wall-clock **hour numbers**
([`CronDate.ts:555-558`](https://github.com/harrisiirak/cron-parser/blob/master/src/CronDate.ts#L555-L558)):

```ts
const previousHour = this.getHours();
this.invokeDateOperation(op, unit);
const currentHour = this.getHours();
const diff = currentHour - previousHour;
if (diff === 2) { ... }
```

There are **two distinct failure modes**, not one.

### 1. Two-hour transition — `diff` overshoots the constant

`Antarctica/Troll`, 2026-03-29, `00:00 → 03:00`, so `diff = 3`. The branch never
fires, `dstStart` stays `null`, no compensation happens.

```js
const it = CronExpressionParser.parse('30 1 * * *', {
  tz: 'Antarctica/Troll',
  currentDate: '2026-03-27T00:00:00',
});
[...Array(4)].forEach(() => console.log(it.next().toISOString()));
```

```
2026-03-27 01:30 +00:00
2026-03-28 01:30 +00:00
2026-03-30 01:30 +02:00   <-- March 29 skipped entirely
2026-03-31 01:30 +02:00
```

### 2. Thirty-minute transition — `diff` depends on the STARTING MINUTE

This is the part I think is new. `Australia/Lord_Howe` shifts **30 minutes**
(`01:30 → 02:30` on 2026-10-04). Because `addHour()` is
`plus({hours:1}).startOf('hour')`, whether the branch fires depends on where
iteration entered the hour:

| start (local) | after `addHour()` | prevH | curH | `diff` | `=== 2` fires? |
| --- | --- | --- | --- | --- | --- |
| `01:00`–`01:29` | `02:30 +11:00` | 1 | 2 | **1** | no |
| `01:30`–`01:59` | `03:00 +11:00` | 1 | 3 | **2** | **yes** |
| `02:30`–`02:59` | `03:00 +11:00` | 2 | 3 | 1 | no |

So for the *same transition* the observation is `1` or `2` depending on the entry
point. Entering before `:30` makes the 30-minute gap arithmetically invisible —
two hour numbers differing by one. Entering at or after `:30` trips the branch and
records `dstStart = 2`, an **hour** value for a **half-hour** gap.

Either way, a daily 02:00 job never runs on the transition day:

```js
const it = CronExpressionParser.parse('0 2 * * *', {
  tz: 'Australia/Lord_Howe',
  currentDate: '2026-10-02T00:00:00',
});
```

```
2026-10-02 02:00 +10:30
2026-10-03 02:00 +10:30
2026-10-05 02:00 +11:00   <-- October 4 skipped entirely
2026-10-06 02:00 +11:00
```

### Why this changes the fix

`currentHour - previousHour` measures a **relabelling of the wall clock**, not the
size of the discontinuity. No constant works for both zones, because the quantity
being compared is the wrong quantity. And `dstStart`/`dstEnd` are typed
`number | null` holding hours
([`CronDate.ts:25-26`](https://github.com/harrisiirak/cron-parser/blob/master/src/CronDate.ts#L25-L26)),
so a 30-minute discontinuity is not representable in the data model at any value.

`getUTCOffset()` already exists at
[`CronDate.ts:327`](https://github.com/harrisiirak/cron-parser/blob/master/src/CronDate.ts#L327)
and its docstring even says *"Useful for detecting DST transition days"* — it just
is not used by the DST detection. Comparing offset deltas in **minutes** looks like
the right primitive.

Verified against tzdata 2025b (Node 24.7.0 / ICU 77.1); offsets cross-checked
identical against tzdata 2025c.

Reproductions: https://github.com/sanjaysah101/port-mortem-cron-parser/blob/main/FINDINGS.md#finding-1--diff--2-is-wall-clock-arithmetic-and-it-is-wrong-twice
