# Pure Go MV2 Writer Design Specification

> Status: Draft | Created: 2026-01-07

## Motivation

The memvid CLI subprocess approach works but has overhead:
- Process spawn latency (~50-100ms per operation)
- npm/node.js runtime dependency
- Serialization overhead (JSON over stdin/stdout)

A pure Go writer eliminates these dependencies and enables:
- In-process writes with minimal latency
- No external dependencies (single binary deployment)
- Direct embedding integration (skip CLI's re-embedding)
- Streaming writes during long sessions

## MV2 Format Summary

Based on [memvid/memvid MV2_SPEC.md](https://github.com/memvid/memvid/blob/main/MV2_SPEC.md):

```
┌─────────────────────────────────────────────────────────────┐
│ Header (4096 bytes)                                          │
│ - Magic: MV2\0 (0x4D 0x56 0x32 0x00)                        │
│ - Version, TOC offset, WAL metadata                         │
├─────────────────────────────────────────────────────────────┤
│ WAL Region (1-64 MB, dynamically sized)                     │
│ - Embedded write-ahead log for crash safety                 │
│ - Entry: sequence | type | payload_len | payload | crc32    │
├─────────────────────────────────────────────────────────────┤
│ Data Segments                                                │
│ - Frames with content, metadata, checksums                  │
│ - Zstd or LZ4 compressed                                    │
├─────────────────────────────────────────────────────────────┤
│ Lex Index (Tantivy segment) - Full-text BM25                │
├─────────────────────────────────────────────────────────────┤
│ Vec Index (HNSW segment) - 384d BGE embeddings              │
├─────────────────────────────────────────────────────────────┤
│ Time Index - Temporal queries                               │
├─────────────────────────────────────────────────────────────┤
│ TOC/Footer (MVTC magic)                                     │
│ - Segment descriptors with offsets/lengths                  │
│ - SHA-256 checksums                                         │
└─────────────────────────────────────────────────────────────┘
```

## Implementation Strategy

### Phase 1: Write-Only Export (MVP)

Goal: Generate MV2 files that `memvid find` can read.

| Component | Go Implementation | Notes |
|-----------|------------------|-------|
| Header | Native | Fixed 4KB, simple binary format |
| WAL | Skip | Not needed for one-shot export |
| Data Segment | Native | Zstd via `github.com/klauspost/compress/zstd` |
| Lex Index | **Challenge** | Tantivy is Rust-only |
| Vec Index | **Challenge** | Custom HNSW or bleve |
| Time Index | Native | Simple frame_id→timestamp mapping |
| TOC | Native | Binary format with checksums |

### Phase 2: Index Solutions

**Option A: Skip Indices (Simplest)**
- Write data + time index only
- No search capability in exported files
- Use for archival/transfer, search via foxctl

**Option B: Bleve for Full-Text**
- Use [blevesearch/bleve](https://github.com/blevesearch/bleve) for BM25
- Different index format than Tantivy (not compatible)
- Would need custom segment type

**Option C: Generate Tantivy-Compatible Index**
- Tantivy uses a documented segment format
- Could generate compatible segments from Go
- High complexity, format may change

**Option D: Hybrid Approach (Recommended)**
- Write data + time index in Go
- Call `memvid index <file>` post-export to build lex/vec indices
- Best of both worlds: fast bulk write, full search

### Phase 3: Full Read/Write Support

Future scope if needed:
- Read existing MV2 files
- Append to existing files
- In-place index updates

## Go Package Design

```go
package mv2

// Writer creates MV2 files from scratch
type Writer struct {
    path       string
    file       *os.File
    header     Header
    frames     []Frame
    timeIndex  []TimeEntry
    options    WriterOptions
}

type WriterOptions struct {
    Compression CompressionType // Zstd (default), LZ4, Raw
    EmbedModel  string          // Optional: generate embeddings
    BuildLex    bool            // Build full-text index
    BuildVec    bool            // Build vector index
}

// Create initializes a new MV2 file
func Create(path string, opts WriterOptions) (*Writer, error)

// Put adds a frame to the file
func (w *Writer) Put(frame Frame) error

// PutBatch efficiently adds multiple frames
func (w *Writer) PutBatch(frames []Frame) error

// Finalize writes indices and TOC, closes file
func (w *Writer) Finalize() error
```

## Binary Encoding Details

### Header (4096 bytes)

```go
type Header struct {
    Magic          [4]byte  // "MV2\0"
    Version        uint16   // Little-endian
    SpecMajor      uint8    // 2
    SpecMinor      uint8    // 1
    FooterOffset   uint64   // TOC position
    WalOffset      uint64   // Always 4096 for new files
    WalSize        uint64   // 0 for export-only
    WalCheckpoint  uint64   // 0
    WalSequence    uint64   // 0
    TocChecksum    [32]byte // SHA-256 of TOC
    Reserved       [4016]byte
}
```

### Frame Binary Format

```go
type FrameData struct {
    FrameID        uint64           // Monotonic
    URILen         uint32
    URI            []byte
    TitleLen       uint32
    Title          []byte
    CreatedAt      uint64           // Unix seconds
    Encoding       uint8            // 0=raw, 1=zstd, 2=lz4
    PayloadLen     uint32
    Payload        []byte           // Compressed content
    PayloadChecksum [32]byte        // SHA-256 of uncompressed
    TagCount       uint32
    Tags           []TagEntry       // key-len, key, val-len, val
    Status         uint8            // 0=active, 1=tombstone
}
```

### Time Index Segment

```go
// Magic: MVTI (0x4D 0x56 0x54 0x49)
type TimeIndexEntry struct {
    FrameID   uint64
    Timestamp uint64  // Unix seconds
    Offset    uint64  // Byte position in data segment
}
```

### TOC/Footer

```go
// Magic: MVTC
type TOC struct {
    Magic         [4]byte  // "MVTC"
    Version       uint16
    SegmentCount  uint32
    Segments      []SegmentDescriptor
    Checksum      [32]byte // SHA-256 of preceding TOC data
}

type SegmentDescriptor struct {
    Type     uint8    // 0x01=data, 0x02=lex, 0x03=vec, 0x04=time
    Offset   uint64
    Length   uint64
    Checksum [32]byte
}
```

## Dependencies

Required:
- `github.com/klauspost/compress/zstd` - Zstd compression
- `crypto/sha256` - Checksums (stdlib)

Optional:
- `github.com/blevesearch/bleve/v2` - Full-text indexing (if Option B)
- Voyage/OpenAI client - Embedding generation

## Implementation Order

1. **Header writer** - Fixed format, easy validation
2. **Frame encoder** - Content compression + checksums
3. **Time index writer** - Simple sequential format
4. **Data segment writer** - Frame batching
5. **TOC writer** - Segment offsets + checksums
6. **Integration** - `Create → Put(s) → Finalize` flow
7. **Validation** - Test with `memvid stats` and `memvid find`

## Compatibility Testing

```bash
# Generate test file with Go writer
go test -run TestMV2Writer -v

# Verify with memvid CLI
memvid stats /tmp/test-go.mv2 --output json
memvid verify /tmp/test-go.mv2

# Test search (if indices built)
memvid find /tmp/test-go.mv2 --query "test" --json
```

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Tantivy index format undocumented | High | Use hybrid approach (Go write, CLI index) |
| Format version changes | Medium | Pin to v2.1, monitor releases |
| HNSW implementation differences | Medium | Skip vec index initially |
| Large file performance | Low | Stream writes, don't buffer all frames |

## Decision: Recommended Approach

**Hybrid Write + CLI Index (Option D)**

1. Go handles: header, data segments, time index, TOC
2. CLI handles: lex index (Tantivy), vec index (HNSW)
3. Post-export: `memvid index <file>` to finalize

Benefits:
- Fast bulk writes (no process spawn per frame)
- Full search compatibility (native Tantivy/HNSW)
- Incremental implementation
- Works immediately, optimize later

Future: If CLI indexing becomes a bottleneck, implement bleve or custom HNSW.

## References

- [MV2_SPEC.md](https://github.com/memvid/memvid/blob/main/MV2_SPEC.md)
- [memvid-cli](https://www.npmjs.com/package/memvid-cli)
- [klauspost/compress](https://github.com/klauspost/compress)
