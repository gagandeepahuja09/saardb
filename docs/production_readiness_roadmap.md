# Production Readiness Roadmap

## Current State
~4.5K lines of code, ~2.3K lines of tests. Working storage layer (WAL → Memtable → SSTable → Compaction), transaction layer (2PL write locks + MVCC reads), and query layer (SQL parser, CREATE TABLE, INSERT, SELECT with primary key, full scan, and secondary index paths).

## Definition of Production Ready
Production ready = correctness + rigorous testing + performance. In that order.

A database that is fast but returns wrong data is worse than a database that is slow but always correct. Hence, correctness first.

---

## Phase 1: Correctness Through Testing (2-3 weekends)

### Concurrent MVCC Correctness
The banking test from blog 10: run multiple transfers in parallel across accounts, verify that total balance is always consistent regardless of when snapshots are taken.
- Multiple readers and writers running concurrently
- Each reader should see a consistent snapshot (total balance = constant)
- No dirty reads, no partial updates visible

### Crash Recovery
- Write N entries, simulate crash (kill process mid-write), verify all committed data is recovered on restart
- Crash during WAL write: partial entry should be detected and truncated
- Crash after WAL write but before memtable update: WAL replay should rebuild memtable correctly
- Crash during memtable flush to SSTable: should recover from WAL

### Edge Cases
- Memtable flush happening during an active transaction: does the transaction still see its own writes?
- Compaction running while reads are ongoing: do readers still see correct versions?
- Rollback after partial writes: are all buffered writes correctly discarded?
- Tombstone handling with MVCC: are deletes versioned correctly? Can a reader with an older snapshot still see a deleted key?

---

## Phase 2: Property-Based Testing (2-3 weekends)

### Linearizability Checking
Use Porcupine (Go library: github.com/anishathalye/porcupine) to verify that the database's operation history is linearizable.
- Record all operations with timestamps
- Feed the history to Porcupine
- Verify that a valid sequential ordering exists

### Random Operation Generator
Generate random sequences of transactions:
- Random mix of PUT, GET, DELETE, BEGIN, COMMIT, ROLLBACK
- Multiple concurrent transactions
- Verify invariants hold after each sequence:
  - Every committed write is readable
  - No uncommitted write is visible to other transactions
  - Snapshot consistency: a read transaction sees a consistent point-in-time view

### How Other Databases Do This
- **Pebble (CockroachDB's LSM)**: metamorphic testing. Run same operations with different configurations (block size, flush threshold, compaction trigger) and verify identical results. Reference: github.com/cockroachdb/pebble/tree/master/metamorphic
- **SQLite**: More test code than production code. Uses OSSFuzz for continuous fuzz testing.
- **FoundationDB**: Deterministic simulation testing. Simulates network partitions, crashes, slow disks in a deterministic framework.

---

## Phase 3: YCSB Benchmarks (2 weekends)

### What is YCSB
Yahoo Cloud Serving Benchmark. Standard benchmark for key-value and database systems. Defines standard workloads with different read/write ratios.

### Workloads to Implement
- **Workload A (Update heavy)**: 50% reads, 50% updates. Simulates session store.
- **Workload B (Read mostly)**: 95% reads, 5% updates. Simulates photo tagging.
- **Workload C (Read only)**: 100% reads. Simulates user profile cache.
- **Workload D (Read latest)**: 95% reads, 5% inserts. Reads skew towards recently inserted records.
- **Workload F (Read-modify-write)**: 50% reads, 50% read-modify-write. Simulates user database with atomic read-update.

### Establish Baseline
Run all workloads, record:
- Throughput (ops/sec)
- Latency (p50, p95, p99)
- Memory usage
- Disk I/O

This gives a "before" to compare against for performance work.

---

## Phase 4: Performance Improvements (3-4 weekends)

### Bloom Filters (biggest win for reads)
Currently a GET that doesn't exist scans every SSTable file. A bloom filter per SSTable can skip files that definitely don't contain the key. This avoids unnecessary disk reads and index lookups.

Expected impact: significant reduction in read latency for missing keys and read-heavy workloads.

### Concurrent Memtable Flush (biggest win for writes)
Currently memtable flush blocks writes. The standard pattern is:
1. When memtable is full, make it immutable
2. Create a new active memtable for incoming writes
3. Flush the immutable memtable to SSTable in the background

This allows writes to continue during flush.

### Block Cache
Currently every SSTable read goes to disk. An LRU cache for frequently accessed data blocks would reduce disk I/O for read-heavy workloads.

### Re-run YCSB
After each improvement, re-run YCSB workloads to measure the actual impact. Compare against Phase 3 baseline.

---

## Phase 5: Advanced (ongoing)

### Write Batching / Group Commit
Currently each WAL write does an fsync. Batching multiple writes into one fsync (like Postgres does) significantly improves write throughput at the cost of a small durability window.

### Leveled Compaction
Currently compaction triggers at 4+ L0 files and merges them. Leveled compaction (L0 → L1 → L2) with size-based triggers reduces read amplification at scale. Each level has a size limit. When a level exceeds its limit, files are merged into the next level.

### Range Delete Support
Efficiently deleting a range of keys. Useful for TTL-based data expiry.

### HammerDB-Style Transactional Workloads
HammerDB (used by TidesDB) runs standard transactional benchmarks like TPC-C and TPC-H. These simulate real-world transactional patterns:
- TPC-C: order processing (new order, payment, delivery, stock level, order status)
- TPC-H: analytical queries on transactional data

This is more relevant once the SQL layer supports more operations (JOINs, GROUP BY, aggregates).

---

## References
- YCSB: github.com/brianfrankcooper/YCSB
- Porcupine (linearizability checker): github.com/anishathalye/porcupine
- Pebble metamorphic testing: github.com/cockroachdb/pebble/tree/master/metamorphic
- HammerDB: hammerdb.com
- Jepsen / Elle: github.com/jepsen-io/elle
- TidesDB benchmarking approach: tidesdb Discord / GitHub
