#!/usr/bin/env python3
"""Create vector index on Turso sessions database for fast similarity search."""

import os
import libsql_experimental as libsql

TURSO_URL = os.environ.get('TURSO_DATABASE_URL')
TURSO_TOKEN = os.environ.get('TURSO_AUTH_TOKEN')

def main():
    if not TURSO_URL or not TURSO_TOKEN:
        print("Error: TURSO_DATABASE_URL and TURSO_AUTH_TOKEN must be set")
        return 1

    print(f"Connecting to Turso: {TURSO_URL}")
    conn = libsql.connect(database=TURSO_URL, auth_token=TURSO_TOKEN)

    # Check current sessions count
    count = conn.execute("SELECT COUNT(*) FROM sessions").fetchone()[0]
    with_embedding = conn.execute("SELECT COUNT(*) FROM sessions WHERE embedding IS NOT NULL").fetchone()[0]
    print(f"\nTotal sessions: {count}")
    print(f"Sessions with embeddings: {with_embedding}")

    # Check if index already exists
    existing = conn.execute("""
        SELECT COUNT(*) FROM sqlite_master
        WHERE type='index' AND name='idx_sessions_embedding_vec'
    """).fetchone()[0]

    if existing > 0:
        print("\nVector index 'idx_sessions_embedding_vec' already exists!")
        return 0

    # Create the vector index
    print("\nCreating vector index 'idx_sessions_embedding_vec'...")
    try:
        conn.execute("""
            CREATE INDEX idx_sessions_embedding_vec
            ON sessions (libsql_vector_idx(embedding))
        """)
        conn.commit()
        print("Vector index created successfully!")
    except Exception as e:
        print(f"Error creating index: {e}")
        return 1

    # Verify the index was created
    verify = conn.execute("""
        SELECT COUNT(*) FROM sqlite_master
        WHERE type='index' AND name='idx_sessions_embedding_vec'
    """).fetchone()[0]

    if verify > 0:
        print("Index verified successfully!")
    else:
        print("Warning: Index creation may have failed")

    conn.close()
    print("\nDone!")
    return 0

if __name__ == '__main__':
    exit(main())
