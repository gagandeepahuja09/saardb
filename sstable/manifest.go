package sstable

import (
	"encoding/json"
	"fmt"
	"os"
)

type manifest struct {
	NextFileId int `json:"next_file_id"`
	// fileNames in manifest indicates the actual order. Example: due to compaction, it is
	// possible that 5.json has older data compared to 4.json
	// FileNames will always have the oldest file first
	FileNames []string `json:"file_names"`
	// todo: MaxTxnId  uint64
}

func (st *SsTable) getManifest() (*manifest, error) {
	filePath := fmt.Sprintf("%s/%s", st.dataFilesDirectory, manifestJsonFileName)
	manifestJsonFile, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	fileInfo, _ := manifestJsonFile.Stat()
	fileSize := fileInfo.Size()
	if fileSize == 0 {
		return &manifest{}, nil
	}
	manifestBuf := make([]byte, fileSize)
	manifestJsonFile.Read(manifestBuf)
	if err != nil {
		return nil, err
	}
	// todo: when we start writing txnId to sstable, we also need to persist maxTxnId in manifest file.
	// and utilise that during application bootup to identify the maxTxnId.

	var manifest manifest
	err = json.Unmarshal(manifestBuf, &manifest)
	return &manifest, err
}

func (st *SsTable) saveManifest() error {
	// todo: when we start writing txnId to sstable, we also need to persist maxTxnId in manifest file.
	// and utilise that during application bootup to identify the maxTxnId.
	manifestJsonBuf, err := json.MarshalIndent(st.manifest, "", " ")
	if err != nil {
		return err
	}
	filePath := fmt.Sprintf("%s/%s", st.dataFilesDirectory, manifestJsonFileName)
	err = os.WriteFile(filePath, manifestJsonBuf, 0644)
	return err
}

func (st *SsTable) getAllFiles(filePaths []string) ([]*os.File, error) {
	ssTableFiles := []*os.File{}
	for _, filePath := range filePaths {
		file, err := os.OpenFile(filePath, os.O_RDONLY, 0644)
		if err != nil {
			return nil, err
		}
		ssTableFiles = append(ssTableFiles, file)
	}
	return ssTableFiles, nil
}
