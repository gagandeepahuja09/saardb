                Dirty  Dirty  Lost   Non-Rep  Phantom  Write
                    Write  Read   Update Read     Read     Skew                                                            
READ UNCOMMITTED     ✓      ✗      ✗      ✗        ✗        ✗
READ COMMITTED       ✓      ✓      ✗      ✗        ✗        ✗                                                              
REPEATABLE READ      ✓      ✓      ✓      ✓        ✗*       ✗                                                            
SERIALIZABLE         ✓      ✓      ✓      ✓        ✓        ✓ 

In blog 4 we discussed and implemented SERIALIZABLE isolation. The problem is that this isolation level has very low adoption due to performance constraints. 

Most production systems use isolation levels like REPEATABLE READ and READ COMMITTED by default. Postgres uses READ COMMITTED as the default isolation level while MySQL uses REPEATABLE READ. 

In the upcoming blogs, we will take a dig at implementing READ COMMITTED isolation which is the default isolation and in the process also explore MVCC. 

## Why 2PL has low adoption?
2PL is a major performance bottleneck due to the fact that readers block writers and writers block readers. Most production user-facing systems are read-heavy in nature. In read-heavy applications, if we are able to ensure that readers and writers are not blocked on each other and only two writers are blocked on each other, we can gain major performance improvements. Let's take an example to solidify what we are saying. If 90% of the database traffic is going to be read traffic, then with 2PL, read transactions would be blocked on some write transaction most of the time. On the other hand, if read transactions don't require a read lock, 90% of our traffic is unaffected by the performance bottleneck due to locks.

While write-write conflicts are non-avoidable in nature, read-write conflicts can be avoided. We will soon see on why write-write conflicts are non-avoidable.

## Write Locks
Write locks are non-negotiable for any application be it with or without databases.

What are dirty writes and why write locks are non-negotiable.


## Read Uncommitted
Go over this in greater detail, implement it and show implementation details.

## Read Committed
Read committed isolation level. We can implement read committed just by utilising write locks and buffered writes.

The only difference between this and 2PL is that there won't be any read lock. Write lock would still be there and we would write everything to the database only during commit. Before that, the writes are stored in memory in specific buffer maps specific to the transaction.

```
  GET "payment_123" returns "5000"
  T2 
    BEGIN

    GET "payment_123" returns "5000" outside T2
    PUT "payment_123" "10000" 
    GET "payment_123" returns "5000" outside T2
    COMMIT
  GET "payment_123" returns "10000"
```

As you can see in above example, we are always reading committed data which is the key requirement for READ COMMITTED isolation level.

While the above approach works well for implementing READ COMMITTED in a key-value store, it is not sufficient for a relational database. This is because of multi-row reads.

Above GET example is a single row read which is an atomic operation. On the other hand, SELECT can read multiple rows which are multiple atomic operations composed sequentially. Due to this, it is possible that we are reading 1 row before a commit and 1 row after a commit.

Let's take an example.
```sql
  SELECT * FROM accounts WHERE id IN (1, 2, 3);
```

We know exactly which 3 rows to read. But we read them sequentially: row 1, then row 2, then row 3. Between reading row 1 and row 3, row 3 could be modified by another transaction. The result becomes a mix of two database states.

Example:

Go over this in greater detail, implement it and show implementation details.
Mention that we can check blog 4 for recap on dirty reads.

In case of multi-row select, we encounter cases where  

SELECT * FROM users WHERE city = 'Delhi';

  This scans multiple rows. During the scan:

  Read row 1: Alice, Delhi       ← scanned

    Writer commits: inserts Bob (Delhi), deletes Alice (Delhi)

  Read row 2: Bob, Delhi         ← scanned

  Without MVCC, this single SELECT sees Alice (pre-commit) AND Bob (post-commit). The statement's result is a mix of two
  different database states

### Building MVCC For Read Committed
MVCC helps with providing a consistent snapshot to a SELECT query.

```sql
  SELECT * FROM users WHERE city = 'Delhi';
```

inserts Bob (Delhi) --> a new version is created but we are still reading v1.

### MVCC Intuition

During reads, we need a consistent snapshot of the database. For that, we need to number each transaction with a transaction id. 

For example:

PUT Alice Delhi XID 50
PUT Charlie Delhi XID 51
PUT Bob Delhi XID 60
DELETE Alice XID 60

While reading, we only read the ones which are created by that time. So, if we need to know the state at XID = 51, I only replay the first 2 and get: Alice, Charlie.

On the other hand, if we need the state at XID = 60, we replay all and get: Charlie, Bob.

We need to maintain all active transaction IDs.

So, for a transaction ID, only the transaction IDs which are <= it are relevant to be seen. But there might be transactions which have lower transaction ID and still active (not yet committed). We should not consider those.

#### Examples
PUT Name Alex 30
PUT Name Bob 32
PUT Name Charlie 33
PUT Name David 35

#### Write Path
1. During the start of the transaction (BEGIN), we need to create a new transaction ID. This transaction ID should be a part of the SSTable and Memtable format.
2. Maintain an array of active transaction ids in-memory. Add current transaction ID during begin and remove it during rollback or commit.
3. During COMMIT, while writing the key-value pairs, we should write the version as part of the serialised value. We are intentionally not keeping it as part of the key, to allow for efficient search by primary key.

#### Read Path
**GET Query**
1. While running a GET query for a specific key and checking SSTable, we usually stop at the first occurrence which is the newest file having the key. But within READ COMMITTED, we need to find the first visible key-value pair.
2. What is the definition of visible? A key-value pair will be visible if the transaction id of the key-value pair is:
  - Less than the transaction id of the current ongoing transaction.
  - Not part of active transaction IDs.
  As soon as a visible transaction ID is found, we should stop the search. Since the newest file is the most up-to-date one, 

**SELECT Query (Non-Primary Indexed)**
In case of SELECT query, we run prefix scan. While running prefix scan, we maintain an in-memory map to ensure that we are only setting the value when we are seeing a key for the first time. If we are seeing a key again, it is an old value.

We need to change the logic here for when should we set the key. We need to apply the same visibility principle. If the key-value pair is visible and not set till now, we update the map.

#### Compaction Path
As of now, during compaction we only keep the newest value. We take 4 files at a time, maintain an in-memory map and only keep the newest value for a key.

Now, when we are going through files, we need to take care of multiple active transactions. Which is the oldest active transaction. 

When we are deleting old value, we cannot delete something which is visible to an active transaction. If something is visible to the oldest active transaction, it must be visible to all transactions.

But we don't need to keep all visible ones. Else, we won't be able to delete anything. Out of the visible ones, pick the highest transaction_id which is less than oldest active transaction id.

Let's say that the 

## Non-repeatable read

## 

## When to use which isolation level
Production system examples

## Code Abstraction to allow easily changing isolation level

## Benchmarking

What is the problem with 2 PL?
Readers block writers and a single writer block readers and writers also block each other. This block behaviour is what slows down our database.
Readers acquire read locks and Writers acquire write locks.

The only way to improve performance is to drop locks entirely
1. So that other reader transactions don't see uncommitted data. But that was solved by the buffered writes solution which we came up with.
2. Yes, buffered write solves it. But what if the writer also commits and the reader needs to read again? Then reader was acting on two different values through their transaction. Is that a problem? This is the repeatable read problem. MVCC solves the repeatable read problem.