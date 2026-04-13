package memvid

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/klauspost/compress/zstd"
)

// MV2 format constants
const (
	MV2Magic       = "MV2\x00"
	TOCMagic       = "MVTC"
	TimeIndexMagic = "MVTI"
	HeaderSize     = 4096
	SpecMajor      = 2
	SpecMinor      = 1
)

// CompressionType for frame payloads
type CompressionType uint8

const (
	CompressionRaw  CompressionType = 0
	CompressionZstd CompressionType = 1
	CompressionLZ4  CompressionType = 2
)

// SegmentType identifies segment kinds in the TOC
type SegmentType uint8

const (
	SegmentData SegmentType = 0x01
	SegmentLex  SegmentType = 0x02
	SegmentVec  SegmentType = 0x03
	SegmentTime SegmentType = 0x04
)

// WriterOptions configures MV2 file creation
type WriterOptions struct {
	Compression CompressionType
}

// DefaultWriterOptions returns sensible defaults
func DefaultWriterOptions() WriterOptions {
	return WriterOptions{
		Compression: CompressionZstd,
	}
}

// Writer creates MV2 files from scratch
type Writer struct {
	path        string
	file        *os.File
	opts        WriterOptions
	encoder     *zstd.Encoder
	nextFrameID uint64

	// In-memory buffers (flushed on Finalize)
	frames         []frameRecord
	dataOffset     uint64 // Current write position in data segment
	dataSegmentPos uint64 // Where data segment starts
}

// frameRecord tracks a written frame
type frameRecord struct {
	id        uint64
	offset    uint64 // Position in data segment
	length    uint64
	timestamp uint64
}

// NewWriter creates a new MV2 file writer
func NewWriter(path string, opts WriterOptions) (*Writer, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}

	w := &Writer{
		path:           path,
		file:           f,
		opts:           opts,
		nextFrameID:    1,
		dataSegmentPos: HeaderSize, // Data starts after header (no WAL for export)
	}

	// Initialize zstd encoder if needed
	if opts.Compression == CompressionZstd {
		enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("create zstd encoder: %w", err)
		}
		w.encoder = enc
	}

	// Write placeholder header (will be updated on Finalize)
	if err := w.writeHeader(0); err != nil {
		f.Close()
		return nil, err
	}

	return w, nil
}

// Put adds a single frame to the MV2 file
func (w *Writer) Put(frame Frame) error {
	return w.PutBatch([]Frame{frame})
}

// PutBatch efficiently adds multiple frames
func (w *Writer) PutBatch(frames []Frame) error {
	for _, f := range frames {
		if err := w.writeFrame(f); err != nil {
			return err
		}
	}
	return nil
}

// writeFrame encodes and writes a single frame
func (w *Writer) writeFrame(f Frame) error {
	frameID := w.nextFrameID
	w.nextFrameID++

	// Compress content
	var payload []byte
	var encoding CompressionType

	switch w.opts.Compression {
	case CompressionZstd:
		payload = w.encoder.EncodeAll([]byte(f.Content), nil)
		encoding = CompressionZstd
	default:
		payload = []byte(f.Content)
		encoding = CompressionRaw
	}

	// Compute checksum of uncompressed content
	checksum := sha256.Sum256([]byte(f.Content))

	// Determine timestamp
	var timestamp uint64
	if !f.CreatedAt.IsZero() {
		timestamp = uint64(f.CreatedAt.Unix())
	} else {
		timestamp = uint64(time.Now().Unix())
	}

	// Record frame position before writing
	startOffset := w.dataOffset

	// Write frame binary data
	// Format: frame_id(8) | uri_len(4) | uri | title_len(4) | title |
	//         created_at(8) | encoding(1) | payload_len(4) | payload |
	//         checksum(32) | tag_count(4) | tags... | status(1)

	buf := make([]byte, 0, 256+len(payload))

	// Frame ID
	buf = binary.LittleEndian.AppendUint64(buf, frameID)

	// URI
	uriBytes := []byte(f.URI)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(uriBytes)))
	buf = append(buf, uriBytes...)

	// Title
	titleBytes := []byte(f.Title)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(titleBytes)))
	buf = append(buf, titleBytes...)

	// Timestamp
	buf = binary.LittleEndian.AppendUint64(buf, timestamp)

	// Encoding
	buf = append(buf, byte(encoding))

	// Payload
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(payload)))
	buf = append(buf, payload...)

	// Checksum
	buf = append(buf, checksum[:]...)

	// Tags
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(f.Tags)))
	for k, v := range f.Tags {
		keyBytes := []byte(k)
		valBytes := []byte(v)
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(keyBytes)))
		buf = append(buf, keyBytes...)
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(valBytes)))
		buf = append(buf, valBytes...)
	}

	// Status (0 = active)
	buf = append(buf, 0)

	// Write to file
	if _, err := w.file.Write(buf); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}

	// Track frame
	w.frames = append(w.frames, frameRecord{
		id:        frameID,
		offset:    startOffset,
		length:    uint64(len(buf)),
		timestamp: timestamp,
	})
	w.dataOffset += uint64(len(buf))

	return nil
}

// Finalize writes indices and TOC, closes the file
func (w *Writer) Finalize() error {
	if w.file == nil {
		return fmt.Errorf("writer already closed")
	}
	defer func() {
		w.file.Close()
		w.file = nil
		if w.encoder != nil {
			w.encoder.Close()
		}
	}()

	// Write time index segment
	timeIndexOffset, timeIndexLen, err := w.writeTimeIndex()
	if err != nil {
		return fmt.Errorf("write time index: %w", err)
	}

	// Write TOC
	tocOffset, err := w.writeTOC(timeIndexOffset, timeIndexLen)
	if err != nil {
		return fmt.Errorf("write TOC: %w", err)
	}

	// Update header with TOC offset
	if err := w.writeHeader(tocOffset); err != nil {
		return fmt.Errorf("update header: %w", err)
	}

	return nil
}

// writeHeader writes the MV2 header
func (w *Writer) writeHeader(tocOffset uint64) error {
	header := make([]byte, HeaderSize)

	// Magic
	copy(header[0:4], MV2Magic)

	// Version (format: major.minor as uint16)
	binary.LittleEndian.PutUint16(header[4:6], uint16(SpecMajor*100+SpecMinor))

	// Spec version
	header[6] = SpecMajor
	header[7] = SpecMinor

	// Footer offset
	binary.LittleEndian.PutUint64(header[8:16], tocOffset)

	// WAL offset (always 4096, but we skip WAL for export)
	binary.LittleEndian.PutUint64(header[16:24], HeaderSize)

	// WAL size, checkpoint, sequence = 0 (no WAL)

	// TOC checksum would go at offset 48, but we compute it in writeTOC

	// Seek to start and write header
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := w.file.Write(header); err != nil {
		return err
	}

	// Seek back to data position
	if _, err := w.file.Seek(int64(w.dataSegmentPos+w.dataOffset), io.SeekStart); err != nil {
		return err
	}

	return nil
}

// writeTimeIndex writes the temporal index segment
func (w *Writer) writeTimeIndex() (offset uint64, length uint64, err error) {
	offset = w.dataSegmentPos + w.dataOffset

	// Seek to position after data
	if _, err := w.file.Seek(int64(offset), io.SeekStart); err != nil {
		return 0, 0, err
	}

	buf := make([]byte, 0, 4+len(w.frames)*24)

	// Magic
	buf = append(buf, []byte(TimeIndexMagic)...)

	// Entry count
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(w.frames)))

	// Entries
	for _, fr := range w.frames {
		buf = binary.LittleEndian.AppendUint64(buf, fr.id)
		buf = binary.LittleEndian.AppendUint64(buf, fr.timestamp)
		buf = binary.LittleEndian.AppendUint64(buf, w.dataSegmentPos+fr.offset)
	}

	if _, err := w.file.Write(buf); err != nil {
		return 0, 0, err
	}

	return offset, uint64(len(buf)), nil
}

// writeTOC writes the table of contents
func (w *Writer) writeTOC(timeIndexOffset, timeIndexLen uint64) (uint64, error) {
	tocOffset, _ := w.file.Seek(0, io.SeekCurrent)

	// Build TOC content (before checksum)
	buf := make([]byte, 0, 256)

	// Magic
	buf = append(buf, []byte(TOCMagic)...)

	// Version
	buf = binary.LittleEndian.AppendUint16(buf, uint16(SpecMajor*100+SpecMinor))

	// Segment count (2: data + time index)
	buf = binary.LittleEndian.AppendUint32(buf, 2)

	// Data segment descriptor
	dataLen := w.dataOffset
	dataChecksum := sha256.Sum256(nil) // TODO: compute actual checksum
	buf = append(buf, byte(SegmentData))
	buf = binary.LittleEndian.AppendUint64(buf, w.dataSegmentPos)
	buf = binary.LittleEndian.AppendUint64(buf, dataLen)
	buf = append(buf, dataChecksum[:]...)

	// Time index segment descriptor
	timeChecksum := sha256.Sum256(nil) // TODO: compute actual checksum
	buf = append(buf, byte(SegmentTime))
	buf = binary.LittleEndian.AppendUint64(buf, timeIndexOffset)
	buf = binary.LittleEndian.AppendUint64(buf, timeIndexLen)
	buf = append(buf, timeChecksum[:]...)

	// Compute TOC checksum
	tocChecksum := sha256.Sum256(buf)
	buf = append(buf, tocChecksum[:]...)

	if _, err := w.file.Write(buf); err != nil {
		return 0, err
	}

	return uint64(tocOffset), nil
}

// Stats returns statistics about the written file
func (w *Writer) Stats() Stats {
	return Stats{
		FrameCount: int64(len(w.frames)),
		FileSize:   int64(w.dataSegmentPos + w.dataOffset),
	}
}
