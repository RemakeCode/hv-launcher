package cpuidmodule

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type sourceRecord struct {
	path   string
	size   int64
	digest [sha256.Size]byte
}

func normalizedSourceDigest(records []sourceRecord) string {
	sort.Slice(records, func(left, right int) bool { return records[left].path < records[right].path })
	hash := sha256.New()
	var length [8]byte
	for _, record := range records {
		binary.BigEndian.PutUint64(length[:], uint64(len(record.path)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(record.path))
		binary.BigEndian.PutUint64(length[:], uint64(record.size))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write(record.digest[:])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func DigestDirectory(root string, limits Limits) (string, error) {
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("registered DKMS source must be a directory")
	}
	records := make([]sourceRecord, 0)
	var total int64
	entries := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		entries++
		if entries > limits.MaxEntries {
			return fmt.Errorf("%w: source contains more than %d entries", ErrResourceLimit, limits.MaxEntries)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: registered source contains %s", ErrUnsafeEntry, path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if len(relative) > limits.MaxPathBytes || strings.Contains(relative, "\\") {
			return fmt.Errorf("%w: source path is too long", ErrResourceLimit)
		}
		if info.Size() > limits.MaxFileBytes || info.Size() > limits.MaxExpandedBytes-total {
			return fmt.Errorf("%w: registered source is too large", ErrResourceLimit)
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		written, copyErr := io.Copy(hash, io.LimitReader(file, limits.MaxFileBytes+1))
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written != info.Size() {
			return fmt.Errorf("registered source changed during digest")
		}
		total += written
		var digest [sha256.Size]byte
		copy(digest[:], hash.Sum(nil))
		records = append(records, sourceRecord{path: relative, size: written, digest: digest})
		return nil
	})
	if err != nil {
		return "", err
	}
	return normalizedSourceDigest(records), nil
}
