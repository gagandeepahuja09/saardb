Isolation is an important property in relational databases to ensure that multiple users and applications can read and write data at the same time without interfering with each other. In blog 4, we discussed isolation and implemented SERIALIZABLE isolation via 2PL (Two-Phase Locking). The problem is that this isolation level has very low adoption due to performance constraints under concurrent workloads.

Most production systems use weaker isolation levels like REPEATABLE READ and READ COMMITTED by default. Postgres uses READ COMMITTED as the default isolation level while MySQL uses REPEATABLE READ.

In the next set of blogs, we will understand why these weaker isolation levels are the default, implement both READ COMMITTED and REPEATABLE READ, and in the process explore MVCC (Multi-Version Concurrency Control).

## Why Is Serializable Isolation Via 2PL Rarely Used?
2PL has a major performance bottleneck due to the fact that readers block writers and writers block readers. Most production user-facing systems are read-heavy in nature. If 90% of the database traffic is going to be read traffic, then with 2PL, read transactions would be blocked on some write transaction most of the time. On the other hand, if read transactions don't get blocked on write transactions, 90% of our traffic is unaffected by the performance bottleneck due to locks.

While write-write conflicts are non-avoidable in nature, read-write conflicts can be avoided.

## Write Locks
Write locks are non-negotiable for any application be it with or without databases. The same concept of shared variable in programming applies here. A "key" is a shared variable and if a transaction was updating some key-value pair and another transaction intervened in between and updated the same key, it would lead to inconsistent or unexpected result for the first transaction. Operations within the transaction and the final result after commit were operating with the assumption that the PUT operation in first transaction was successful with the expected value.

Given that write locks are unavoidable, we will try to see if it is possible to remove read locks. If we can remove read locks while keeping write locks, we get the performance benefit from the previous section: readers and writers no longer block each other.

## Isolation Levels and Anomalies

|                        | Dirty Read | Dirty Write | Non-Repeatable Read | Lost Update | Phantom Read | Write Skew |
|------------------------|------------|-------------|---------------------|-------------|--------------|------------|
| **Read Uncommitted**   | Possible   | Prevented   | Possible            | Possible    | Possible     | Possible   |
| **Read Committed**     | Prevented  | Prevented   | Possible            | Possible    | Possible     | Possible   |
| **Repeatable Read**    | Prevented  | Prevented   | Prevented           | Prevented   | Possible     | Possible   |
| **Serializable**       | Prevented  | Prevented   | Prevented           | Prevented   | Prevented    | Prevented  |

Above table provides a view of what isolation anomalies each isolation level solves. We covered Dirty Read, Lost Update and Write Skew in blog 4. We will cover the remaining anomalies as they become relevant through this blog series.

The interesting thing is that READ COMMITTED doesn't solve for four of the six isolation anomalies and REPEATABLE READ doesn't solve for two. Yet these are the defaults that most production databases ship with and most organisations never change.

### Why Do Default Isolation Levels Not Solve For All Anomalies?

The unsolved anomalies: Lost Update, Write Skew and Phantom Read require specific conditions to trigger. Both Lost Update and Write Skew need a **read-then-write** pattern where two transactions concurrently read and then write to overlapping data. While read-then-write patterns are common in applications (check balance then transfer, check inventory then place order), two transactions hitting the same keys concurrently with this pattern is rare enough that most applications don't encounter these anomalies in practice.

For the cases where they do arise, weaker isolation levels provide an escape hatch: **SELECT FOR UPDATE**. Adding `FOR UPDATE` to a SELECT query locks the returned rows with a write-lock, preventing other transactions from reading or modifying them until the current transaction completes. For example, in the lost update scenario from blog 4, if both T1 and T2 used `SELECT balance FOR UPDATE` instead of a regular GET, the second transaction would block until the first commits and hence preventing the lost update.

Hence, the common pattern in production is pragmatic: use the default weaker isolation level which handles the majority of cases, and reach for explicit locking via `SELECT FOR UPDATE` in the specific code paths that need stronger guarantees.

This is exactly what we will implement in this blog series. We will start with Read Committed, then build up to Repeatable Read. Both are built on top of MVCC, which we will explore in detail.

## Read Committed

### No Dirty Reads
The only guarantee provided by Read Committed is that there are no dirty (or uncommitted) reads and no dirty writes.
