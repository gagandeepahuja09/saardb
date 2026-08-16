What's strong

  - Write locks as "shared variable" (line 20) — excellent analogy, connects directly to what developers already understand
  from programming
  - KV store → SQL transition (lines 54-65) — clean narrative arc showing why buffered writes work for GET but not for
  multi-row SELECT
  - Both examples are realistic — checkout and merchant settlement are genuine OLTP scenarios, not analytics
  - The conclusion (lines 119-122) — tying it to performance cost AND developer burden is the right framing

  What needs fixing

  Line 8: "very low adoption due to performance constraints" — from our research, it's more about retry handling, historical
  inertia, and ecosystem defaults. Not just performance. SSI performance is actually comparable.

  Line 29: "Read Committed only solves for one: dirty reads" — should also mention dirty writes. Write locks prevent dirty
  writes at every level including READ COMMITTED. You explained this well on line 20 but don't connect it to the anomaly
  table.

  Line 42-44: The dry-run formatting is confusing — "returns 5000 for Transaction T1" reads like T2 is returning something
  for T1. Maybe restructure as:

  T1: GET "payment_123" → returns "5000"
  T2: BEGIN
  T2: PUT "payment_123" "10000"
  T1: GET "payment_123" → returns "5000" (reads committed value, not T2's buffer)
  T2: GET "payment_123" → returns "10000" (reads own buffer)
  T2: COMMIT
  T1: GET "payment_123" → returns "10000" (T2 committed, now visible)

  Line 87: Add the key insight — "the total charged was a value that never existed in the database at any point in time."
  That's the punchline.

  Line 112: "can cause reporting problem" undersells it. The merchant could make business decisions (withdraw money, plan
  payroll) based on a wrong settlement amount. Not just a reporting issue.

  Line 124: Typo — "except" should be "expect".

  What's missing

  The strongest technical proof: Erik Darling and Paul White demonstrated that SQL Server's non-MVCC READ COMMITTED actually
  misses rows and double-counts rows during concurrent index modifications. One sentence referencing this would strengthen
  your argument: "SQL Server's default READ COMMITTED (without MVCC) is known to produce wrong results during concurrent
  scans — rows can be missed or counted twice due to index modifications during the scan."

  The SQL Server RCSI connection: Worth mentioning that SQL Server offers READ COMMITTED both with and without MVCC — proving
   that MVCC for READ COMMITTED is an engineering choice, not a standard requirement. PostgreSQL and MySQL chose MVCC. SQL
  Server left it opt-in.