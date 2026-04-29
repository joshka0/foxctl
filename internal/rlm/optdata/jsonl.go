package optdata

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// WriteTrajectoryRecordsJSONL writes one trajectory record per line.
func WriteTrajectoryRecordsJSONL(w io.Writer, records []TrajectoryRecord) error {
	enc := json.NewEncoder(w)
	for _, record := range records {
		record = cloneRecord(record)
		if err := record.Validate(); err != nil {
			return err
		}
		if err := enc.Encode(record); err != nil {
			return fmt.Errorf("encode RLM optimization trajectory record: %w", err)
		}
	}
	return nil
}

// BuildTrajectoryRecordsJSONL returns JSONL bytes for trajectory records.
func BuildTrajectoryRecordsJSONL(records []TrajectoryRecord) ([]byte, error) {
	var builder strings.Builder
	if err := WriteTrajectoryRecordsJSONL(&builder, records); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

// ParseTrajectoryRecordsJSONL decodes newline-delimited trajectory records.
func ParseTrajectoryRecordsJSONL(r io.Reader) ([]TrajectoryRecord, error) {
	records := []TrajectoryRecord{}
	err := StreamTrajectoryRecordsJSONL(r, func(record TrajectoryRecord) error {
		records = append(records, record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

// StreamTrajectoryRecordsJSONL streams newline-delimited trajectory records.
func StreamTrajectoryRecordsJSONL(r io.Reader, emit func(TrajectoryRecord) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record TrajectoryRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return fmt.Errorf("decode RLM optimization trajectory record: %w", err)
		}
		if err := record.Validate(); err != nil {
			return err
		}
		if emit != nil {
			if err := emit(cloneRecord(record)); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan RLM optimization trajectory records: %w", err)
	}
	return nil
}

// AppendTrajectoryRecordFile appends one trajectory record to a JSONL file.
func AppendTrajectoryRecordFile(path string, record TrajectoryRecord) error {
	return AppendTrajectoryRecordsFile(path, []TrajectoryRecord{record})
}

// AppendTrajectoryRecordsFile appends trajectory records to a JSONL file.
func AppendTrajectoryRecordsFile(path string, records []TrajectoryRecord) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("append RLM optimization trajectory record: path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	return WriteTrajectoryRecordsJSONL(file, records)
}

// LoadTrajectoryRecordsFile reads trajectory records from a JSONL file.
func LoadTrajectoryRecordsFile(path string) ([]TrajectoryRecord, error) {
	file, err := os.Open(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ParseTrajectoryRecordsJSONL(file)
}
