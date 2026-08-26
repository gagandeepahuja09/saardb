## Todo:
* There is also a bugfix pending. The bug was around offset. When reading the last block
    * How was this identified: 
* Improvements:
    * Write data blocks to SS Table in the following format: 
* Ask Cursor to also find bugs in the code.

## Test Plan
* Add test cases around partial and corrupted write.

# CURSOR SUGGESTED TEST PLAN
* As you can see, the purpose of this repo is for me to learn the internals of database and I have made some progress till LSM table and compaction. I also need good high quality production level test cases. Evaluate the code and tell me what all tests I should write and in what prioriy order. We can also identify some bugs also and see that with failing tests and then fix them.

I'll explore your codebase to understand the implementation and then suggest a comprehensive test plan. Let me start by reading the main components.

[9 tools called]

Let me also check the main.go and the design documentation to understand the intended behavior better:

[2 tools called]

Let me check the manifest file to understand the file path format:

[1 tool called]

Excellent! I've analyzed your codebase thoroughly. This is a solid LSM-tree implementation with WAL, Memtable, SSTable, and compaction. Let me provide a comprehensive analysis with **bugs identified** and a **prioritized test plan**.

---

## 🐛 Bugs Identified

### **Bug 1: Double Path Prefix in `getAllLogFiles`** (Critical: Done)

```51:58:sstable/manifest.go
func (st *SsTable) getAllLogFiles() ([]*os.File, error) {
	fileNames := st.manifest.FileNames
	ssTableFiles := []*os.File{}
	for _, fileName := range fileNames {
		filePath := fmt.Sprintf("%s/%s", st.dataFilesDirectory, fileName)
		file, err := os.OpenFile(filePath, os.O_RDONLY, 0644)
		if err != nil {
			return nil, err
```

Your manifest stores full paths like `"data_files_sstable/8.log"`, but `getAllLogFiles` prepends the directory again, creating `data_files_sstable/data_files_sstable/8.log`. This will break on restart.

---

### **Bug 2: Index Out of Bounds in `linearSearchFile`** (Critical)

```394:399:sstable/sstable.go
	entries := strings.Split(string(buf), "\n")
	for _, payload := range entries {
		cmds := strings.Split(payload, " ")
		if cmds[1] == key {
			return cmds[2], nil
		}
```

No length check before accessing `cmds[1]` and `cmds[2]`. Empty lines or malformed entries will cause a panic.

---

### **Bug 3: Newline in WAL Value Parsing** (Critical: Done)

```108:115:db/db.go
		line := string(payload)
		args := strings.Split(line, " ")
		if len(args) != 3 {
			return nil, errors.New("Expected exactly 2 arguments for PUT command\n")
		}
		key := args[1]
		value := args[2]
		memTable.Put(key, value)
```

The WAL stores `"PUT key value\n"`, so `args[2]` will include the trailing newline (`"value\n"` instead of `"value"`). You should use `strings.TrimSpace(line)` before splitting.

---

### **Bug 4: Last Block Read May Overflow Into Index Block** (Medium)

```324:328:sstable/sstable.go
		endOffset := ssTableIndex[lowerBoundSliceIndex].offset + st.blockLength
		if lowerBoundSliceIndex < len(ssTableIndex)-1 {
			// todo: it is safer to have endOffset as start of index offset.
			// this can potentially lead to issue as more than
			endOffset = ssTableIndex[lowerBoundSliceIndex+1].offset
		}
```

For the last block, it uses `offset + blockLength`, but the last block may be smaller. You should use `indexOffset` (the footer value) as the end boundary.

---

### **Bug 5: `linearSearchFile` Uses `Read` Instead of `ReadAt`** (Medium)

```386:389:sstable/sstable.go
func (st *SsTable) linearSearchFile(file *os.File, key string) (string, error) {
	stat, _ := file.Stat()
	fileSize := stat.Size()
	buf := make([]byte, fileSize)
	_, err := file.Read(buf)
```

`file.Read()` reads from the current file position, not from the start. After multiple reads, the position moves and this will read garbage. Should use `file.ReadAt(buf, 0)`.

---

### **Bug 6: Race Condition in `Put`** (High: Done)

```63:75:db/db.go
func (db *DB) Put(key, value string) error {
	if err := db.writeToWal(key, value); err != nil {
		return errors.New("Something went wrong")
	}
	db.memTable.Put(key, value)

	if db.memTable.ShouldFlush() {
		if err := db.createSsTableAndClearWalAndMemTable(); err != nil {
			return err
		}
	}
	return nil
}
```

Multiple concurrent `Put` calls can:
1. Race on `memTable.Put` (BTree is not thread-safe)
2. Multiple goroutines may pass `ShouldFlush()` and trigger multiple flushes

---

### **Bug 7: Compaction Error Silently Ignored** (Medium)

```62:65:sstable/compaction.go
	compactedMap, err := st.buildCompactedMap(filesToCompact)
	if err != nil {
		slog.Error("COMPACTED_MAP_BUILD_FAILED", "error", err.Error())
	}
```

Errors are logged but execution continues, potentially corrupting data.

---

## 📋 Prioritized Test Plan

### **Priority 1: Unit Tests for Core Components (Must Have)**

#### **1.1 WAL Tests** (`wal/wal_test.go`) [75% Done]
```go
// Test cases to write:
func TestWal_WriteAndReadSingleEntry(t *testing.T)
func TestWal_WriteAndReadMultipleEntries(t *testing.T)
func TestWal_ReadFromEmptyFile(t *testing.T)
func TestWal_Clear(t *testing.T)
func TestWal_ChecksumValidation(t *testing.T)
func TestWal_CorruptedLength(t *testing.T)          // Corrupt the length bytes
func TestWal_CorruptedPayload(t *testing.T)         // Corrupt the payload
func TestWal_CorruptedChecksum(t *testing.T)        // Corrupt the checksum
func TestWal_PartialWriteLength(t *testing.T)       // Truncate after length bytes
func TestWal_PartialWritePayload(t *testing.T)      // Truncate mid-payload
func TestWal_PartialWriteChecksum(t *testing.T)     // Truncate mid-checksum
func TestWal_LargePayload(t *testing.T)             // Test 1MB limit
func TestWal_SpecialCharactersInPayload(t *testing.T)
```

#### **1.2 Memtable Tests** (`memtable/memtable_test.go`)
```go
func TestMemtable_PutAndGet(t *testing.T)
func TestMemtable_GetNonExistent(t *testing.T)
func TestMemtable_OverwriteKey(t *testing.T)
func TestMemtable_SizeTracking(t *testing.T)
func TestMemtable_SizeTrackingOnOverwrite(t *testing.T)
func TestMemtable_ShouldFlush(t *testing.T)
func TestMemtable_Clear(t *testing.T)
func TestMemtable_Iterate_SortedOrder(t *testing.T)
func TestMemtable_EmptyIterate(t *testing.T)
func TestMemtable_LargeNumberOfKeys(t *testing.T)
```

---

### **Priority 2: SSTable Core Tests (Critical Path)**

#### **2.1 SSTable Read/Write Tests** (`sstable/sstable_test.go`)
```go
func TestSsTable_WriteAndRead(t *testing.T)
func TestSsTable_ReadNonExistentKey(t *testing.T)
func TestSsTable_MultipleFiles_NewestWins(t *testing.T)
func TestSsTable_IndexBlockParsing(t *testing.T)
func TestSsTable_FooterParsing(t *testing.T)
func TestSsTable_BinarySearchLowerBound(t *testing.T)
func TestSsTable_LastBlockBoundary(t *testing.T)       // Tests Bug #4
func TestSsTable_LinearSearch(t *testing.T)
func TestSsTable_EmptyFile(t *testing.T)
func TestSsTable_SingleEntry(t *testing.T)
func TestSsTable_BlockBoundaries(t *testing.T)         // Keys at exact block boundaries
```

#### **2.2 Manifest Tests** (`sstable/manifest_test.go`)
```go
func TestManifest_CreateNew(t *testing.T)
func TestManifest_SaveAndLoad(t *testing.T)
func TestManifest_FileOrder(t *testing.T)
func TestManifest_PathHandling(t *testing.T)           // Tests Bug #1
func TestManifest_CorruptedJson(t *testing.T)
```

---

### **Priority 3: Integration Tests (End-to-End)**

#### **3.1 DB Integration Tests** (`db/db_test.go`)
```go
func TestDB_BasicPutGet(t *testing.T)
func TestDB_MemtableFlushToSsTable(t *testing.T)
func TestDB_RecoveryFromWAL(t *testing.T)              // Restart and verify data
func TestDB_RecoveryFromWALWithNewlineIssue(t *testing.T)  // Tests Bug #3
func TestDB_GetFromSsTableAfterFlush(t *testing.T)
func TestDB_OverwriteKeyAcrossFlushes(t *testing.T)
func TestDB_LargeDataset(t *testing.T)
func TestDB_Close(t *testing.T)
```

---

### **Priority 4: Compaction Tests (Complex Logic)**

#### **4.1 Compaction Tests** (`sstable/compaction_test.go`)
```go
func TestCompaction_TriggerThreshold(t *testing.T)
func TestCompaction_MergesAllFiles(t *testing.T)
func TestCompaction_NewestValueWins(t *testing.T)
func TestCompaction_SortedOutput(t *testing.T)
func TestCompaction_AtomicSwap(t *testing.T)
func TestCompaction_FilesDeletedAfter(t *testing.T)
func TestCompaction_ManifestUpdated(t *testing.T)
func TestCompaction_ConcurrentReads(t *testing.T)      // Read during compaction
func TestCompaction_ConcurrentWrites(t *testing.T)     // Write during compaction
func TestCompaction_ErrorHandling(t *testing.T)        // Tests Bug #7
```

---

### **Priority 5: Concurrency Tests (Production Quality)**

```go
func TestDB_ConcurrentPuts(t *testing.T)               // Tests Bug #6
func TestDB_ConcurrentGets(t *testing.T)
func TestDB_ConcurrentPutsAndGets(t *testing.T)
func TestDB_ConcurrentFlushes(t *testing.T)
func TestSsTable_ConcurrentReadsWithCompaction(t *testing.T)
```

---

### **Priority 6: Edge Cases & Error Handling**

```go
func TestDB_EmptyKey(t *testing.T)
func TestDB_EmptyValue(t *testing.T)
func TestDB_SpacesInValue(t *testing.T)                // Will break current format!
func TestDB_VeryLongKey(t *testing.T)
func TestDB_VeryLongValue(t *testing.T)
func TestDB_SpecialCharacters(t *testing.T)
func TestSsTable_CorruptedDataBlock(t *testing.T)
func TestSsTable_CorruptedIndexBlock(t *testing.T)
func TestSsTable_MissingFile(t *testing.T)
```

---

## 🔧 Recommended Bug Fixes (Quick Reference)

| Bug | Fix |
|-----|-----|
| #1 Double Path | Store only filename in manifest, not full path |
| #2 Index OOB | Add `len(cmds) >= 3` check before accessing |
| #3 Newline | Use `strings.TrimSpace(line)` before splitting |
| #4 Last Block | Use `indexOffset` from footer for last block end |
| #5 Read vs ReadAt | Change `file.Read(buf)` to `file.ReadAt(buf, 0)` |
| #6 Race Condition | Add mutex to DB struct or use sync.RWMutex |
| #7 Silent Error | Return error or don't continue after compaction error |

---

## 🎯 Suggested Test Implementation Order

1. **Start with WAL tests** - Foundation of durability
2. **Memtable tests** - Simple, isolated unit tests
3. **SSTable write/read tests** - Core persistence logic
4. **Manifest path bug test** - Will fail, fix Bug #1
5. **WAL parsing test with newlines** - Will fail, fix Bug #3
6. **Linear search test** - Will fail, fix Bug #2 and #5
7. **DB integration tests** - End-to-end verification
8. **Compaction tests** - Complex but important
9. **Concurrency tests** - Production readiness

Would you like me to write the actual test implementations for any of these, starting with the highest priority ones? If so, please switch to **Agent mode** and I can create the test files for you.

------------------
17 Jan

gagandeep.ahuja@RZP1834 golang-db % go test -coverprofile=c.out ./...
ok      github.com/golang-db    12.241s coverage: 0.0% of statements
ok      github.com/golang-db/db 55.102s coverage: 70.9% of statements
        github.com/golang-db/memtable           coverage: 0.0% of statements
        github.com/golang-db/sstable            coverage: 0.0% of statements
ok      github.com/golang-db/wal        0.987s  coverage: 77.6% of statements

/usr/local/go/bin/go run main.go