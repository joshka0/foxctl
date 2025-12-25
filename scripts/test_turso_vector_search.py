#!/usr/bin/env python3
"""Test vector search on Turso sessions database."""

import os
import json
import sys
import libsql_experimental as libsql

GEMINI_API_KEY = os.environ.get('GEMINI_API_KEY')
TURSO_URL = os.environ.get('TURSO_DATABASE_URL')
TURSO_TOKEN = os.environ.get('TURSO_AUTH_TOKEN')

def get_embedding(text):
    """Get embedding from Gemini API."""
    import requests
    url = f"https://generativelanguage.googleapis.com/v1beta/models/gemini-embedding-001:embedContent?key={GEMINI_API_KEY}"
    payload = {
        "model": "models/gemini-embedding-001",
        "content": {"parts": [{"text": text}]}
    }
    try:
        response = requests.post(url, json=payload, timeout=10)
        response.raise_for_status()
    except requests.Timeout:
        print("Error: Request timed out after 10 seconds")
        sys.exit(1)
    except requests.RequestException as e:
        print(f"Error: Request failed: {e}")
        sys.exit(1)
    data = response.json()
    return data['embedding']['values']

def vector_to_string(embedding):
    """Convert embedding list to vector string format."""
    return '[' + ','.join(f'{v:.6f}' for v in embedding) + ']'

def test_self_similarity():
    """Test that sessions are most similar to themselves using existing embeddings."""
    print(f"\n{'='*60}")
    print("Test: Self-similarity (each session should be most similar to itself)")
    print('='*60)

    conn = libsql.connect(database=TURSO_URL, auth_token=TURSO_TOKEN)

    # Get all sessions with embeddings
    sessions = conn.execute("""
        SELECT id, summary, vector_extract(embedding) as embedding_str
        FROM sessions
        WHERE embedding IS NOT NULL
        LIMIT 5
    """).fetchall()

    print(f"\nTesting {len(sessions)} sessions...")

    for session_id, summary, embedding_str in sessions:
        # Find the most similar session to this one
        result = conn.execute(f"""
            SELECT
                id,
                summary,
                vector_distance_cos(embedding, vector('{embedding_str}')) as distance
            FROM sessions
            WHERE embedding IS NOT NULL
            ORDER BY distance ASC
            LIMIT 1
        """).fetchone()

        most_similar_id, most_similar_summary, distance = result
        similarity = 1 - distance

        is_self = session_id == most_similar_id
        status = "✓ PASS" if is_self else "✗ FAIL"

        print(f"\n{status}: Session {session_id[:8]}...")
        print(f"   Query summary: {(summary or '')[:60]}...")
        print(f"   Most similar: {most_similar_id[:8]}... (similarity: {similarity:.4f})")
        if not is_self:
            print(f"   Expected self-match but got: {(most_similar_summary or '')[:60]}...")

    conn.close()

def test_cross_similarity():
    """Test similarity search between different sessions."""
    print(f"\n{'='*60}")
    print("Test: Cross-session similarity search")
    print('='*60)

    conn = libsql.connect(database=TURSO_URL, auth_token=TURSO_TOKEN)

    # Pick a session about CGO/testing
    source = conn.execute("""
        SELECT id, summary, vector_extract(embedding) as embedding_str
        FROM sessions
        WHERE summary LIKE '%CGO%' OR summary LIKE '%test%'
        LIMIT 1
    """).fetchone()

    if not source:
        print("No CGO/test-related sessions found")
        return

    source_id, source_summary, embedding_str = source
    print(f"\nSource session: {source_id}")
    print(f"Summary: {source_summary[:100]}...")

    # Find similar sessions (excluding self)
    print("\nFinding similar sessions...")
    results = conn.execute(f"""
        SELECT
            id,
            summary,
            vector_distance_cos(embedding, vector('{embedding_str}')) as distance
        FROM sessions
        WHERE embedding IS NOT NULL AND id != '{source_id}'
        ORDER BY distance ASC
        LIMIT 5
    """).fetchall()

    print("\nTop 5 similar sessions:")
    for i, (session_id, summary, distance) in enumerate(results, 1):
        similarity = 1 - distance
        summary_preview = (summary or '')[:80]
        print(f"\n   {i}. Similarity: {similarity:.4f}")
        print(f"      ID: {session_id}")
        print(f"      Summary: {summary_preview}...")

    conn.close()

def test_vector_top_k():
    """Test vector_top_k with index for fast similarity search."""
    print(f"\n{'='*60}")
    print("Test: vector_top_k indexed search")
    print('='*60)

    conn = libsql.connect(database=TURSO_URL, auth_token=TURSO_TOKEN)

    # Check if index exists
    idx_exists = conn.execute("""
        SELECT COUNT(*) FROM sqlite_master
        WHERE type='index' AND name='idx_sessions_embedding_vec'
    """).fetchone()[0]

    if not idx_exists:
        print("Vector index does not exist. Run create_turso_vector_index.py first.")
        return

    print("Vector index 'idx_sessions_embedding_vec' exists.")

    # Get an embedding to use as query
    source = conn.execute("""
        SELECT id, summary, vector_extract(embedding) as embedding_str
        FROM sessions
        WHERE embedding IS NOT NULL
        LIMIT 1
    """).fetchone()

    if not source:
        print("No sessions with embeddings found")
        return

    source_id, source_summary, embedding_str = source
    print(f"\nQuery session: {source_id}")
    print(f"Summary: {(source_summary or '')[:80]}...")

    # Use vector_top_k for indexed search
    # Note: vector_top_k only returns rowid, we must compute distance ourselves
    print("\nSearching with vector_top_k (indexed)...")
    import time
    start = time.time()
    results = conn.execute(f"""
        SELECT s.id, s.summary,
               vector_distance_cos(s.embedding, vector('{embedding_str}')) as distance
        FROM vector_top_k('idx_sessions_embedding_vec', '{embedding_str}', 5) vt
        JOIN sessions s ON s.rowid = vt.id
    """).fetchall()
    indexed_time = time.time() - start

    print(f"Indexed search time: {indexed_time*1000:.2f}ms")
    print(f"\nTop 5 results (indexed):")
    for i, (session_id, summary, distance) in enumerate(results, 1):
        similarity = 1 - distance
        summary_preview = (summary or '')[:60]
        print(f"   {i}. Similarity: {similarity:.4f} - {summary_preview}...")

    # Compare with non-indexed search
    print("\nSearching with vector_distance_cos (full scan)...")
    start = time.time()
    results2 = conn.execute(f"""
        SELECT id, summary,
               vector_distance_cos(embedding, vector('{embedding_str}')) as distance
        FROM sessions
        WHERE embedding IS NOT NULL
        ORDER BY distance ASC
        LIMIT 5
    """).fetchall()
    scan_time = time.time() - start

    print(f"Full scan time: {scan_time*1000:.2f}ms")

    # Check if results are the same
    indexed_ids = [r[0] for r in results]
    scan_ids = [r[0] for r in results2]
    if indexed_ids == scan_ids:
        print("\n✓ Both methods return the same results")
    else:
        print("\n! Results differ between methods")
        print(f"   Indexed: {indexed_ids}")
        print(f"   Scan: {scan_ids}")

    conn.close()

def test_with_query(query):
    """Test vector search with a text query (requires GEMINI_API_KEY)."""
    if not GEMINI_API_KEY:
        print(f"\n[Skipping query test - GEMINI_API_KEY not set]")
        return

    print(f"\n{'='*60}")
    print(f"Query: {query}")
    print('='*60)

    # Get query embedding
    print("\n1. Generating query embedding...")
    query_embedding = get_embedding(query)
    print(f"   Dimensions: {len(query_embedding)}")

    # Connect to Turso
    print("\n2. Connecting to Turso...")
    conn = libsql.connect(database=TURSO_URL, auth_token=TURSO_TOKEN)

    # Test vector_distance_cos function
    vector_str = vector_to_string(query_embedding)

    print("\n3. Searching for similar sessions (cosine distance)...")
    results = conn.execute(f"""
        SELECT
            id,
            summary,
            vector_distance_cos(embedding, vector('{vector_str}')) as distance
        FROM sessions
        WHERE embedding IS NOT NULL
        ORDER BY distance ASC
        LIMIT 5
    """).fetchall()

    print("\n4. Results:")
    for i, (session_id, summary, distance) in enumerate(results, 1):
        similarity = 1 - distance
        summary_preview = (summary or '')[:100]
        print(f"\n   {i}. Similarity: {similarity:.4f} (distance: {distance:.4f})")
        print(f"      ID: {session_id}")
        print(f"      Summary: {summary_preview}...")

    conn.close()

def test_db_stats():
    """Show database statistics."""
    print(f"\n{'='*60}")
    print("Database Statistics")
    print('='*60)

    conn = libsql.connect(database=TURSO_URL, auth_token=TURSO_TOKEN)

    # Count sessions
    count = conn.execute("SELECT COUNT(*) FROM sessions").fetchone()[0]
    with_embedding = conn.execute("SELECT COUNT(*) FROM sessions WHERE embedding IS NOT NULL").fetchone()[0]

    print(f"\nTotal sessions: {count}")
    print(f"Sessions with embeddings: {with_embedding}")

    # Check embedding dimensions
    sample = conn.execute("""
        SELECT vector_extract(embedding) as v
        FROM sessions
        WHERE embedding IS NOT NULL
        LIMIT 1
    """).fetchone()

    if sample and sample[0]:
        # Count dimensions from the vector string
        dims = sample[0].count(',') + 1
        print(f"Embedding dimensions: {dims}")

    conn.close()

def main():
    if not TURSO_URL or not TURSO_TOKEN:
        print("Error: TURSO_DATABASE_URL and TURSO_AUTH_TOKEN must be set")
        return 1

    # Database stats
    test_db_stats()

    # Self-similarity test (no API key needed)
    test_self_similarity()

    # Cross-similarity test (no API key needed)
    test_cross_similarity()

    # Test vector_top_k with index
    test_vector_top_k()

    # Query-based test (needs API key)
    if GEMINI_API_KEY:
        test_with_query("CGO and Go testing issues")
        test_with_query("React Native UI design")
    else:
        print("\n[Skipping query-based tests - set GEMINI_API_KEY to enable]")

    print("\n" + "="*60)
    print("Vector search tests complete!")
    print("="*60)
    return 0

if __name__ == '__main__':
    exit(main())
