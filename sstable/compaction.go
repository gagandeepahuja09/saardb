package sstable

// todo: handle concurrent reads and writes between Compaction and regular Get, Write
// functions
import (
	"encoding/binary"
	"errors"
	"log/slog"
	"os"
)

type valueTxnId struct {
	value string
	txnId uint64
}

func (st *SsTable) ShouldRunCompaction() bool {
	st.mutex.RLock()
	defer st.mutex.RUnlock()
	return !st.compacting && len(st.firstLevelFiles) >= 4
}

// builds a compactedEntry struct containing (key, value, txnId) formed from all the
// (key value, txnId) pairs present in the files. We only keep the newest txnId for a key.
func (st *SsTable) buildCompactedMap(files []*os.File) (map[string]valueTxnId, error) {
	// todo: as of now, we will only keep the newest version after compaction.
	// in a subsequent PR, this will be updated to keep the txnId >= oldestActiveTxnId
	// + newest values of txnId for each key if txnId < oldestActiveTxnId
	compactedMap := map[string]valueTxnId{}
	for i := len(files) - 1; i >= 0; i-- {
		file := files[i]
		indexOffset, err := st.getIndexOffset(file)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, indexOffset)
		_, err = file.ReadAt(buf, 0)
		if err != nil {
			return nil, err
		}
		for i := 0; i < len(buf); {
			if i+8 > len(buf) {
				return nil, errors.New("unexpected error while reading txnId during compaction")
			}
			txnId := binary.BigEndian.Uint64(buf[i : i+8])
			i += 8
			key, err := extractValueFromSsTable(buf, i)
			if err != nil {
				return nil, err
			}
			i += (4 + len(key))
			value, err := extractValueFromSsTable(buf, i)
			if err != nil {
				return nil, err
			}
			i += (4 + len(value))
			currValueTxnId, ok := compactedMap[key]
			if !ok || currValueTxnId.txnId < txnId {
				compactedMap[key] = valueTxnId{
					value: value,
					txnId: txnId,
				}
			}
		}
	}
	return compactedMap, nil
}

func (st *SsTable) RunCompaction() {
	// 1. compacting flag set and unset
	st.mutex.Lock()
	st.compacting = true
	st.mutex.Unlock()

	defer func() {
		st.mutex.Lock()
		st.compacting = false
		st.mutex.Unlock()
	}()

	// 2. build compacted entries
	st.mutex.RLock()
	filesToCompact := make([]*os.File, len(st.firstLevelFiles))
	copy(filesToCompact, st.firstLevelFiles)
	st.mutex.RUnlock()
	slog.Info("COMPACTION_STARTED", "files_to_be_compacted_count", len(filesToCompact))
	compactedMap, err := st.buildCompactedMap(filesToCompact)
	if err != nil {
		slog.Error("COMPACTED_MAP_BUILD_FAILED", "error", err.Error())
		return
	}

	// 3. get sorted keys. compacted file needs to have all keys in sorted order
	// and during compaction we lost the order and that needs to be fixed.
	sortedKeys := sortedKeys(compactedMap)

	// 4. create iterator function which calls the callback for each key-value-transactionId combination
	//  in sorted and compacted map
	iterator := func(fn func(key, value string, txnId uint64)) {
		for _, key := range sortedKeys {
			fn(key, compactedMap[key].value, compactedMap[key].txnId)
		}
	}

	// 5. write to the compacted file
	compactedFile, err := st.NewFile()
	if err != nil {
		slog.Error("COMPACTED_FILE_CREATE_FAILED", "error", err.Error())
	}
	compactedIndexOffset, compactedIndexBlock, err := st.writeToFile(compactedFile, iterator)
	if err != nil {
		slog.Error("COMPACTED_FILE_WRITE_FAILED", "error", err.Error())
	}

	slog.Info("COMPACTED_FILE_WRITE_SUCCESSFUL", "file_name", compactedFile.Name())

	// 6. atomic swap of files array and indexes array
	st.atomicSwap(compactedFile, filesToCompact, compactedIndexBlock, compactedIndexOffset)

	slog.Info("COMPACTED_FILE_ATOMIC_SWAP_SUCCESSFUL", "files_to_compact_count", len(filesToCompact))

	// 7. delete old files
	for _, file := range filesToCompact {
		file.Close()
		os.Remove(file.Name())
	}
}

// takes the compacted file, old files array and current state of files array to construct the new ssTables array and sets it.
// similar behaviour done for indexes array.
// the old files / index block / index offset will not be kept after atomic swap as those have now been compacted.
// while the new files which were not part of compaction will get added.
func (st *SsTable) atomicSwap(compactedFile *os.File, oldFiles []*os.File, compactedIndexBlock []indexBlockEntry, compactedIndexOffset int) {
	st.mutex.Lock()
	defer st.mutex.Unlock()

	oldFilesMap := map[string]bool{}

	for _, file := range oldFiles {
		oldFilesMap[file.Name()] = true
	}

	currentFiles := st.firstLevelFiles

	// position is important, compactedFile is older than the newly created files
	swappedFiles := []*os.File{compactedFile}
	fileNames := []string{compactedFile.Name()}
	swappedIndexBlocks := [][]indexBlockEntry{compactedIndexBlock}
	swappedIndexOffsets := []int{compactedIndexOffset}

	for i, file := range currentFiles {
		if !oldFilesMap[file.Name()] {
			swappedFiles = append(swappedFiles, file)
			swappedIndexBlocks = append(swappedIndexBlocks, st.indexBlocks[i])
			swappedIndexOffsets = append(swappedIndexOffsets, st.indexOffsets[i])
			fileNames = append(fileNames, file.Name())
		}
	}

	st.firstLevelFiles = swappedFiles
	st.indexBlocks = swappedIndexBlocks

	st.manifest.FileNames = fileNames
	st.saveManifest()
}

// todo: we should have a test to assert that compaction captures all keys and those keys are in sorted order.
// UT would have helped in 2 ways:
// 1. clearly tell what the issue is
// 2. set the standard even for an agent. Even LLM could have made same mistake especially for something which is entirely
// new and with no documentation.
