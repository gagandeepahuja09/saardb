Now we can see the real issue. "payments" is found but "refunds" is not. Both are stored under the same catalog key (something like
  __tables__), with refunds being a newer version.

  Here's a debugging question for you: when your SSTable has multiple entries with the same key but different txnIds, which one does
  getValueFromSsTableDataBlock return?

  Look at line 475:
  if currentKey == key {
      return currentValue, nil  // returns immediately on FIRST match
  }

Issue: TestAppRestart was failing. Both payments and refunds were not being returned in the list of tables.

Claude helped identified the root cause:
```
  Your memtable sorts entries by key ascending, then txnId ascending. So when flushed to SSTable:

  __tables__, txnId=1, value="payments,"           ← first in file
  __tables__, txnId=3, value="payments,refunds,"   ← second in file

  The GET finds the first match (txnId=1, "payments,") and returns immediately — never sees the second entry with refunds.

  This is why it worked with existing WAL — WAL replay puts both versions in memtable, and your memtable Get() scans all versions and
  returns the latest. But SSTable GET returns the first match, which is the oldest version.
```

__tables__ would have 2 separate versions. During memtable we ensure that we always pick the latest version. While implementing for sstable, we assumed that since we are reading from the newest file first, newest version would always be picked. But within a file, there can be multiple versions and latest version within a file would generally be at the end of the file.

This also means that we need to have better tests for both cases:
1. Case 1: SSTable GET always picks the latest version for that key tests.
2. Case 2: SSTable GET always picks the latest version for each key.

```
The log confirms it. You see _catalog with txnId=1 ("payments") but there's no _catalog with a higher txnId containing
  "payments,refunds,".

  The bug is in your compaction dedup logic. With MVCC, a single SSTable file can have multiple entries for the same key (different
  txnIds). Your memtable flushes ALL versions to the SSTable:

  Within one SSTable file (sorted by key, then txnId ascending):
    _catalog, txnId=1, "payments,"              ← oldest version, first in file
    _catalog, txnId=4, "payments,refunds,"      ← newest version, second in file

  Your compaction iterates through entries and does:
  if !keySeen[key] {
      keySeen[key] = true
      compactedEntries = append(compactedEntries, entry)
  }
```

When GET reads from the compacted file, it uses st.indexOffsets[0] (old file's index offset) as the data block end boundary — wrong   
  size for the new file. The buffer is either too large (reads into index block bytes) or too small (truncates entries), causing the
  parsing panic.                                                                                                                        
                                                                                                                                      
  Add the missing line:

  st.firstLevelFiles = swappedFiles
  st.indexBlocks = swappedIndexBlocks                                                                                                   
  st.indexOffsets = swappedIndexOffsets  // ← add this
                                                                                                                                        
  This bug existed before your MVCC changes — but it only manifests when compaction actually changes the file layout significantly,     
  which happens more with MVCC (multiple versions = different file sizes).
                                                                                                                                        
✻ Baked for 2m 53s                                                                                                                    

❯ how did you catch the bug?                                                                                                            
                             
⏺ By following the panic stack trace backwards:                                                                                         
                                                                                                                                      
  1. Panic is in getValueFromSsTableDataBlock — parsing fails mid-entry, meaning the buffer has wrong data or wrong size                
  2. Buffer size depends on dataBlockEndOffset — which comes from st.indexOffsets[i]. So if indexOffsets is wrong, the buffer is wrong
  3. What updates indexOffsets? — atomicSwap after compaction. Checked if it sets all three arrays (firstLevelFiles, indexBlocks,       
  indexOffsets)                                                                                                                         
  4. Found that swappedIndexOffsets is built but never assigned — the variable is created, populated, and then silently discarded       
                                                                                                                                        
  The general debugging pattern: when parsing fails with "not enough bytes," the data isn't corrupt — the buffer boundaries are wrong.  
  Trace backwards from the buffer size to find where the boundaries come from.

Before MVCC:                                                                                                                          
  - 50 unique keys → memtable has 50 entries → SSTable has 50 entries                                                                   
  - Compaction merges 4 files of ~50 entries each → compacted file has ~50 entries (deduped)                                            
  - Input files and compacted file are roughly the same size                                
  - Stale index offset is close to the actual offset → buffer is slightly wrong but parsing happens to work                             
                                                                                                                                      
  After MVCC:                                                                                                                           
  - 50 unique keys × 12 versions each → memtable has 600 entries → SSTable has 600 entries                                              
  - Compaction merges files with 600 entries → keeps only 1 version per key → compacted file has 50 entries
  - Compacted file is 12x smaller than input files                                                                                      
  - Stale index offset (from the old big file) points way past the actual data in the small compacted file → buffer reads into the index
   block → garbage bytes → panic
                                                                                                                                        
  Before MVCC:                                                                                                                        
    Old file index offset: ~5000 bytes                                                                                                  
    Compacted file index offset: ~5000 bytes                                                                                            
    Difference: small → bug hidden
                                                                                                                                        
  After MVCC:                                                                                                                         
    Old file index offset: ~12000 bytes (600 entries)
    Compacted file index offset: ~1000 bytes (50 entries)                                                                               
    Difference: huge → buffer reads 11000 bytes of garbage → panic
                                                                                                                                        
  Same bug existed on master. MVCC just made the size difference dramatic enough to expose it.                                          
  
  And yes — this is exactly the kind of bug that a unit test for compaction would catch. A test that writes data, runs compaction, then 
  reads back every key would have caught this regardless of MVCC.

  ou're right. TestGetAndPutInBulk does exactly that — writes 378 keys, waits for compaction, reads back. It passed on master because  
  with unique keys, the compacted file is roughly the same size as the inputs, so the stale index offset was close enough.              
                                                                                                                                        
  The missing test was: writes to the same keys repeatedly, triggering compaction, then reads back. That's what your new                
  TestSsTableGetPicksLatestTxnIdWithCompaction does — and it caught the bug immediately.                                                
                                                                                                                                        
  The lesson: tests with only unique keys don't exercise the "compacted file is much smaller than input files" path. You need tests with
   repeated keys to expose size-difference bugs. This was always possible even without MVCC (overwriting the same key via separate
  PUTs), but MVCC made it the default behavior.