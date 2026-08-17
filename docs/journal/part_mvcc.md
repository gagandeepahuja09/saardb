                Dirty  Dirty  Lost   Non-Rep  Phantom  Write
                    Write  Read   Update Read     Read     Skew                                                            
READ UNCOMMITTED     ✓      ✗      ✗      ✗        ✗        ✗
READ COMMITTED       ✓      ✓      ✗      ✗        ✗        ✗                                                              
REPEATABLE READ      ✓      ✓      ✓      ✓        ✗*       ✗                                                            
SERIALIZABLE         ✓      ✓      ✓      ✓        ✓        ✓ 

In blog 4 we discussed and implemented SERIALIZABLE isolation. The problem is that this isolation level has very low adoption due to performance constraints. 

Most production systems use isolation levels like REPEATABLE READ and READ COMMITTED by default. Postgres uses READ COMMITTED as the default isolation level while MySQL uses REPEATABLE READ. 

In the current blog, we will take a dig at implementing both READ COMMITTED and REPEATABLE READ isolation levels and in the process also explore MVCC. 

## Why 2PL has low adoption?
2PL is a major performance bottleneck due to the fact that readers block writers and writers block readers. Most production user-facing systems are read-heavy in nature. In read-heavy applications, if we are able to ensure that readers and writers are not blocked on each other and only two writers are blocked on each other, we can gain major performance improvements. Let's take an example to solidify what we are saying. If 90% of the database traffic is going to be read traffic, then with 2PL, read transactions would be blocked on some write transaction most of the time. On the other hand, if read transactions don't require a read lock, 90% of our traffic is unaffected by the performance bottleneck due to locks.

While write-write conflicts are non-avoidable in nature, read-write conflicts can be avoided.

## Write Locks
Write locks are non-negotiable for any application be it with or without databases. The same concept of shared variable in programming applies here. A "key" is a shared variable and if a transaction was updating some key-value pair and another transaction intervened in between and updated the same key, it would lead to inconsistent or unexpected result for the first transaction as the rest of the operations within the transaction and the final result after commit were operating with the assumption that the PUT operation in first transaction was successful with the expected value.

Given that write locks are unavoidable, we will try to see if it is possible to remove read locks.

## Read Committed

### No Dirty Reads
The only guarantee provided by Read Committed is that there are no dirty (or uncommitted) reads. There are multiple isolation anomalies and we only walked through three of them in blog 4.

Out of all the isolation anomalies, Read Committed only solves for one: dirty reads.

In order to ensure that we are always reading committed data, we can keep an in-memory write buffer visible only to the respective transaction. Those values are only visible to other transactions post commit.

If we correlate this to blog 4, this is very much similar to the 2 phase-locking which we implemented with the only difference being that we stop taking any read locks so that readers and writers are not blocked on each other.

Let's do a dry-run to prove that this solves the dirty read isolation anomaly.
```
  Transaction T1
    GET "payment_123" returns "5000"
  
  Transaction T2 
    BEGIN
    GET "payment_123" returns "5000" for Transaction T1
    PUT "payment_123" "10000" 
    GET "payment_123" returns "5000" for Transaction T1
    GET "payment_123" returns "10000" for Transaction T2
    COMMIT
  
  Transaction T1
    GET "payment_123" returns "10000"
```

In the above example, transaction T1 always reads committed data which is guaranteed by a combination of in-memory buffer for transaction T2 and write-lock acquired by T2 during PUT operation.

### Other Issues Solved By READ COMMITTED In Production Databases

While, this solves the requirements for READ COMMITTED isolation level, this is not generally how Postgres and MySQL implement this isolation level. Let's take a few examples to understand why:

Let's look at following SELECT query:
```sql
  SELECT * FROM accounts WHERE id IN (1, 2, 3);
```

We read 3 rows sequentially. Between reading row 1, 2 and 3, row 3 could be modified by another transaction. The result becomes a mix of two database states.

Above GET example is a single row read which is an atomic operation. On the other hand, SELECT can read multiple rows which are multiple atomic operations composed sequentially. Due to this, it is possible that we are reading 1 row before a commit and 1 row after a commit. This leads to us not reading a consistent database state. Ideally, we would want to read a consistent database state which was there when the SELECT query start and not influenced by writes happening in between.

A natural question that emerges is that, why is it necessary to read a consistent database state?

Let's take a could of real production examples to understand why: 

**Example 1: Checkout from cart**
Assume that you have a checkout service computing the final order value basis the price and quantity of items in cart. The query would look like:
```sql
  SELECT SUM(p.price * c.quantity) as total
  FROM cart_items c
  JOIN products p ON c.product_id = p.id
  WHERE c.cart_id = 'abc';
```
cart_items would require running a JOIN with products table to get the price of the product.

A background job or an admin could be applying discount on any of the products. Leading to its price changing:

```sql
  UPDATE products SET price = price * 0.8 WHERE id IN (1, 2, 3, 4, 7, 8, 15);
```
All of the readers having any of the respective product in cart at that time and checking out the product would run the above SELECT query. While the SELECT query is running, the UPDATE query is also running in parallel for the common product_id(s). Some product could have old price as the SELECT for that row was first before UPDATE and some could have the new price as the SELECT for that row was fired after UPDATE. 
This could lead to a price which the buyer didn't expect and a loss of revenue for the seller as the buyer had seen and agreed to the price before the discount.
Update could also be for removing discounts, leading to a higher price that what the buyer thought and hence poor buyer experience.

In both cases, the product experience would have been correct and better if we had a consistent database state for the SELECT query as if no UPDATE query was running.

**Example 2: Check Pending Settlement For Merchant**

Payment gateway aggregators requirements like Stripe and Razorpay require carrying out settlements for merchants and also showing a view of pending and cleared settlements.

A SELECT query like below could be running to show the merchant a view of their pending settlement amount.
```sql
  SELECT SUM(amount) as pending_settlement_amount
  FROM settlements
  WHERE merchant_id = 5 AND status = 'pending';
```

Parallely, a background job could be running to update the settlement status of the settlements which are not pending.
```sql
  UPDATE ledger_entries SET status = 'cleared' WHERE merchant_id = 5 AND settlement_id IN (4, 7, 10);
```

Assuming that there were 10 pending settlements for merchant_id "5" in above example when the SELECT query was fired. Out of the 10, 3 were updated by the background job. But since, both were running in parallel, it is possible that out of the 3 whose status was changed as cleared, the SELECT query had already read 2 of the rows as pending and for the remaining, the status was read as cleared and hence was removed from SUM(amount).

In this case as well, the merchant saw a settlement SUM which was not theoretically possible as either all of its 3 settlements should have been cleared together or all of them could be pending.

This inconsistent view can cause reporting problem for the merchant.

### Need For Consistent Snapshot
Both these examples show why it is important to have a consistent snapshot view of the database and why this is especially a problem in multi-row reads.

A consistent snapshot means that SELECT queries are able to take a snapshot of the database at the start of the query itself and relying on that instead of the result being impacted by an UPDATE or INSERT or DELETE running in parallel.

We could solve this problem by take explicit locks during reads as well but that comes at the cost of: 

1. Peformance
2. Developer Burden: Developers need to think about whether this use case requires viewing a consistent snapshot or not. If developers miss out on any edge-cases, it could lead to subtle bugs in the system which are bound to happen at scale.

To solve for these problems, both MySQL and Postgres ensure that a consistent snapshot of the database is available during reads even if this requirement is not a strict adherence for READ COMMITTED isolation level. And both Postgres and MySQL except this or even a more stricter isolation level to be default for their databases.

## MVCC (Multi-Version Concurrency Control)
There are two ways to provide a consistent snapshot of the database during reads:
1. Locking writes
2. Maintaining version of each write.

We already discarded approach 1 due to performance constraints. We will be understanding approach 2 in greater details which is exactly what MVCC does.

Assume every write in the database is tagged with a transaction ID. This transaction ID (aka version) is an always increasing number. Higher the transaction ID, the more recently the transaction started. 
Now assume that before starting a SELECT query, we record the current transaction ID as its snapshot. During the scan, we only reads values whose transaction ID is less than or equal to the snapshot. This way, we are effectively seeing the database as it was when the query began, ignoring any concurrent writes.

Let's do a dry run of this:
```sql
  T1 (TID=11): SELECT * FROM users WHERE id IN (1, 2, 3)

  Database state:
    id=1, name="Alice",   written by TID=5  ← TID 5 <= 11, visible ✓
    id=2, name="Bob",     written by TID=8  ← TID 8 <= 11, visible ✓
    id=3, name="Charlie", written by TID=8  ← TID 8 <= 11, visible ✓

  T2 (TID=12): UPDATE users SET name="David" WHERE id=3
    → creates new version: id=3, name="David", written by TID=12

  T1 continues scanning:
    id=3 has two versions:
      TID=12: 12 > 11 → INVISIBLE
      TID=8:  8 <= 11 → VISIBLE, return "Charlie"

  Result: Alice, Bob, Charlie ✓ (consistent, unaffected by T2) 
```

As can be seen in the above example, since T2 (TID = 12) did not affect the result of the query as TID = 11 < 12 and hence TID = 12 is invidisible to transaction T1.

Let's take another example:

```sql

  Assume that TID=10 is already running but not yet committed
  (TID=10): UPDATE users SET name="David" WHERE id=3

  T1 (TID=11): SELECT * FROM users WHERE id IN (1, 2, 3)

  Database state:
    id=1, name="Alice",   written by TID=5  ← TID 5 <= 11, visible ✓
    id=2, name="Bob",     written by TID=8  ← TID 8 <= 11, visible ✓
    id=3, name="Charlie", written by TID=8  ← TID 8 <= 11, visible ✓

  T1 continues scanning and :
    id=3 has two versions:
      TID=12: 12 > 11 → INVISIBLE
      TID=8:  8 <= 11 → VISIBLE, return "Charlie"

  Result: Alice, Bob, Charlie ✓ (consistent, unaffected by T2) 
```

The above example doesn't cover cases where a transaction is already active before the read started. T1 has transaction ID = 11 but let's say transaction ID = 7 is still active. If TID = 7 is committed during the time T1 is active, it could impact the result of T1.
Hence, apart from checking transaction ID version, we should also check for active transactions. If a transaction is active, we should not read that key-value pair.

A version is only visible if the transaction ID of the key-value pair we are interested in reading is:
1. Less than or equal to the transaction ID of the current version to be read.
2. Not an active transaction. 

## Implementing MVCC
Let's tie this up to how we can implement MVCC.

### Write Path

**Write To SSTable**
Given that SSTables already are append-only in nature, we don't require maintaining separate versions. The only difference is that while writing the key-value pair, we also write a version or transaction_id as part of the serialised value itself.

**Write to Memtable**
The above approach doesn't work with memtable as it stores a key-value pair only once. Since one key can have only one associated value in a memtable, we would have to store the version in the memtable key itself.

In order to generalise it, we can keep version as part of key for both memtable and SSTable.

For both table writes and secondary indexes writes we need to have the version as part of the key.

### Read Path

#### GET Path
The read order remains the same: we first check for memtable. If not found in memtable, we check from the newest SSTable to the oldest SSTable.

**Memtable Search**
While searching in memtable, we need to carry out a prefix-scan for the respective key. Since the data is sorted, the oldest version would be present first.

During regular GET, if a key is found in memtable, we don't need to check the SSTable. In this case as well, the newest version would always exist on memtable.

We will first carry out a prefix scan in memtable for the required key and check from the newest version to the oldest. We can also apply binary search in a way such that we only check less than or equal to the read version. The first version that we encounter that is not an active version is the required key-value pair.

If the required key is not found in Memtable, we would need to search in SSTable.

**SSTable Search**

The first version

**SELECT Path**

**SELECT Index Path**

Once we pull that 

But memtable only has one value.

What happens to indexes.

Internal Note: Why active transactions should not be a global variable?

### Compaction Path

### Operational, Storage, Latency Overhead

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

But we don't need to keep all visible ones. Else, we won't be able to delete anything. Out of the visible ones for a key, pick the highest transaction_id which is less than oldest active transaction id.

Let's say that the oldest active transaction id = 55

If during compaction, 
current_id >= 55 --> keep
current_id < 55 --> keep only the newest xid (just less than 55)

How to find this txn_id which is just less than the oldest active transaction id. Maintain a max variable. 

We would need to maintain a map of key to the max active transaction id seen which is less than the oldest active txn_id. Along with that, we also need to store the entire row value in the map.

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