# MVCC Compaction: Deriving the approach from scratch

## The problem

We have multiple versions of each key across SSTable files. Compaction merges files and needs to delete versions that no one will ever read. Which versions are safe to delete?

## When is a version needed?

A version `(key, txnId)` is needed if some living transaction would pick it when reading that key.

Recap of the read rule: transaction T reads a key by picking the **largest txnId that is <= T.id and not in T.activeSnapshot**.

## Brute force

For each `(key, txnId)`, go through every living transaction and ask: "would you pick this version?"

```
For each (key, txnId):
    needed = false
    For each living transaction T:
        Is txnId <= T.id?
        Is txnId NOT in T.activeSnapshot?
        Is there no better version (higher txnId, also visible to T) for this key?
        If all three: needed = true, stop

    If not needed: delete
```

This is correct. But can we actually run this in practice?

## Why brute force is problematic

There are three layers of problems:

**Problem 1: Access to snapshots.**

Each transaction's `activeSnapshot` lives on that transaction's struct, which is being actively used by a goroutine running queries. To read it safely, you need synchronization. You could take a lock, copy all snapshots at the start of compaction, and release the lock. This is a one-time cost and is solvable.

**Problem 2: The check itself is expensive.**

SSTable files can contain millions of `(key, txnId)` entries. There can be hundreds or thousands of active transactions. The brute force is:

```
For each (key, txnId) in SSTable:        -- millions
    For each living transaction:           -- thousands
        Check visibility + shadowing       -- requires scanning other versions of same key
```

That's millions x thousands checks. Compaction is a background process — it should be lightweight and not consume significant CPU. This cross-product makes it expensive.

**Problem 3: The "shadowing" check requires global context.**

The third condition — "is there a better version for this key visible to T?" — means you can't evaluate a single `(key, txnId)` in isolation. You need to know all other versions of the same key and their visibility to T. This turns a per-entry check into a per-entry-per-key-per-transaction check, adding another dimension of complexity.

These problems don't make brute force impossible, but they make compaction slow. And slow compaction is a real problem:

1. **Reads get slower.** A GET checks files from newest to oldest until it finds the key. More files = more files to check. PrefixScan is worse — it has to scan across all files.

2. **Disk usage grows.** Dead versions that should have been cleaned up are still sitting on disk. With heavy write workloads updating the same keys, this bloat can be significant.

3. **Compaction falls further behind.** If compaction is slow and writes keep coming, new SSTable files are created faster than compaction can merge them. The backlog grows, making the next compaction even larger and slower. This is a feedback loop.

So slow compaction isn't just "background process takes longer" — it directly degrades read performance and disk usage, and can snowball.

To put the cost in concrete terms: brute force is `entries × transactions × versions_per_key`. If you have 1 million entries across SSTable files, 1000 active transactions, and an average of 5 versions per key, that's 1M × 1K × 5 = 5 billion checks. For a background process, that's unacceptable.

Ideally we want something that's just `entries × 1` — a single pass through the entries, with a constant-time decision per entry. That's why we need a simpler and more efficient approach — one that avoids checking individual transactions entirely.

## Looking for a shortcut

The brute force is expensive because we check every version against every transaction. What if there were cases where we could skip the per-transaction check entirely?

Ask: **when do all transactions agree about a version?**

If all transactions agree that a version is visible, we don't need to check them individually. And if all of them can see the same set of versions for a key, they all pick the same one (the largest), so we know exactly which ones are shadowed.

## Which versions are visible to ALL transactions?

A version `txnId` is visible to transaction T when:
- `txnId <= T.id` (not from the future)
- `txnId` is not in `T.activeSnapshot` (not uncommitted)

For a version to be visible to **every** living transaction, both conditions must hold for **every** T.

**Condition 1: `txnId <= T.id` for every T.**

This means `txnId` must be <= the smallest T.id among all living transactions. Call that value `minId`. If `txnId <= minId`, then it's automatically <= every other T.id too (since minId is the smallest).

**Condition 2: `txnId` not in any T's activeSnapshot.**

If `txnId < minId`, can it be in anyone's active snapshot? No. If txnId were still an active transaction, it would be a living transaction with id < minId. But minId is the smallest living transaction id. Contradiction. So `txnId` must be committed, which means it's not in any active snapshot.

**Conclusion:** every version with `txnId < minId` is visible to every living transaction.

(Note: we didn't set out to find `minId`. It emerged naturally from asking "which versions does everyone agree on?")

## What does this give us?

For a given key, look at all versions with `txnId < minId`. Every living transaction can see all of them. The read rule picks the largest visible one. So every transaction picks the same version: the one with the max txnId below minId.

All other versions below minId for that key are permanently shadowed. No living transaction picks them. No future transaction will either (future transactions have even higher ids, so they see even more versions, and the max-below-minId version still wins among this group).

**These shadowed versions are dead. Safe to delete.**

## What about versions with txnId >= minId?

These don't satisfy our "visible to ALL transactions" property. Some transactions might see them, others might not — depending on individual T.id values and active snapshots.

Example:
```
key "name": v50, v52, v55
minId = 50, living transactions: {50, 55, 58}

Txn 50: v52 and v55 are > 50, invisible. Picks v50.
Txn 55: v55 is in its active snapshot (invisible). v52 <= 55, not active. Picks v52.
Txn 58: v55 in its active snapshot (invisible). Picks v52.
```

Different transactions pick different versions. To figure out which are safe to delete, we'd need to go back to the brute force — checking each transaction's snapshot individually.

Is it worth it? This zone only contains versions from `minId` to now — a small window of recent writes. The storage savings from deleting a few versions here are tiny compared to the zone below minId (which contains the entire history of the database). The complexity of inspecting individual snapshots isn't justified.

**Keep all versions >= minId.**

## The final rule

```
txnId < minId   -->  keep only the max txnId per key, delete the rest
txnId >= minId  -->  keep all
```

## Why long-running transactions are bad for compaction

Compaction always works — it always cleans up zone A (below minId). But minId is pinned to the oldest living transaction. If one transaction sits open at txnId 500 while the rest of the system is at txnId 10000, minId stays at 500. Zone B (keep everything) spans from 500 to 10000 — 9500 txnIds worth of versions that compaction can't touch, even though only one transaction needs them.

The longer a transaction stays open, the more versions accumulate that can't be deleted. This is exactly the problem Postgres calls **table bloat** — VACUUM (Postgres's compaction) can't clean up rows still visible to a long-running transaction. It's one of Postgres's biggest operational headaches.

This is why the standard advice is: keep transactions short. Not because compaction stops, but because it becomes much less effective.

## Summary

We arrived at this by asking:
1. Which versions can we reason about without checking individual transactions? (The ones visible to everyone.)
2. What property makes a version visible to everyone? (txnId < the smallest living transaction id.)
3. Among those universally visible versions, which are needed? (Only the largest per key.)
4. What about the rest? (Can't simplify without brute force. Small zone. Not worth it. Keep all.)

---- 

Your intuition is there and the flow is good. A few things I'd tighten:

  1. The jump from brute force to "just check the oldest" needs the bridge.

  Line 46 says "let's just check if the oldest active transaction needs it" but doesn't explain why that's sufficient. The key reasoning
   (which you have in my doc) is: if a version with txnId < minActiveId is visible to minActiveId, it's visible to every transaction —
  because every transaction has a higher id and the txnId is committed (can't be in anyone's active snapshot). That's the justification
  for why checking one transaction covers all of them for this zone.

  2. The txnId > minActiveId reasoning could be sharper.

  Line 51 says "that would require checking every transactionId." Worth adding the concrete reason: different transactions have
  different snapshots and different ids, so they pick different versions — there's no single transaction you can check to represent all
  of them.

  3. Minor: line 49 has a subtle inaccuracy.

  "minActiveId just needs all (key, txnId) where txnId <= minActiveId and for those txnId, only keep the latest value" — it should
  clarify "latest visible value", meaning latest txnId that's not in minActiveId's active snapshot. Though in practice, everything below
   minActiveId is committed, so "latest" and "latest visible" are the same. Worth noting explicitly.

  4. The cons section could mention the feedback loop.

  Long-running transactions don't just keep data around — they make the next compaction larger too, since more zone B data accumulates.
  This is Postgres's table bloat problem.

  Otherwise the structure is solid: problem → brute force → why it's slow → observe we only need to check the oldest → derive the rule →
   pros/cons of the tradeoff. Nothing major missing.
