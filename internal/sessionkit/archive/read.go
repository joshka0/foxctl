package archive

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
)

// ReadChunksFromArchive reads specific line indices from a gzipped JSONL archive.
// It returns a map of index to raw line bytes.
func ReadChunksFromArchive(archivePath string, indices []int) (map[int][]byte, error) {
	results := make(map[int][]byte, len(indices))
	if len(indices) == 0 {
		return results, nil
	}

	targetSet := make(map[int]struct{}, len(indices))
	for _, idx := range indices {
		targetSet[idx] = struct{}{}
	}

	file, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzReader.Close()

	scanner := bufio.NewScanner(gzReader)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	lineNum := 0
	for scanner.Scan() {
		if _, ok := targetSet[lineNum]; ok {
			line := make([]byte, len(scanner.Bytes()))
			copy(line, scanner.Bytes())
			results[lineNum] = line
			if len(results) == len(targetSet) {
				break
			}
		}
		lineNum++
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan archive: %w", err)
	}

	return results, nil
}
