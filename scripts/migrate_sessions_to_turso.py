#!/usr/bin/env python3
"""Migrate sessions with embeddings from local SQLite to Turso."""

import os
import sqlite3
import struct
import libsql_experimental as libsql

def blob_to_vector_string(blob):
    """Convert embedding blob (float32 array) to vector string format."""
    if not blob:
        return None

    # Each float32 is 4 bytes
    num_floats = len(blob) // 4
    floats = struct.unpack(f'<{num_floats}f', blob)

    # Format as vector string: [f1,f2,f3,...]
    return '[' + ','.join(f'{f:.6f}' for f in floats) + ']'

def main():
    turso_url = os.environ.get('TURSO_DATABASE_URL')
    turso_token = os.environ.get('TURSO_AUTH_TOKEN')
    local_db = os.environ.get('LOCAL_SESSIONS_DB',
                              os.path.expanduser('~/.agentctl/storage/sessions.db'))

    if not turso_url or not turso_token:
        print("Error: TURSO_DATABASE_URL and TURSO_AUTH_TOKEN must be set")
        return 1

    print(f"Opening local database: {local_db}")
    local = sqlite3.connect(local_db)
    local.row_factory = sqlite3.Row

    print(f"Connecting to Turso: {turso_url}")
    turso = libsql.connect(database=turso_url, auth_token=turso_token)

    # Query sessions with embeddings
    cursor = local.execute("""
        SELECT id, workspace_path, project_name, summary, accomplished, decisions,
               gotchas, tags, key_files, started_at, ended_at, embedding,
               embedding_model, created_at, updated_at
        FROM sessions
        WHERE embedding IS NOT NULL
    """)

    migrated = 0
    for row in cursor:
        session_id = row['id']
        embedding_blob = row['embedding']
        vector_str = blob_to_vector_string(embedding_blob)

        if not vector_str:
            print(f"Skipping session {session_id}: invalid embedding")
            continue

        try:
            # Insert using vector() function for the embedding
            turso.execute("""
                INSERT OR REPLACE INTO sessions
                (id, workspace_path, project_name, summary, accomplished, decisions,
                 gotchas, tags, key_files, started_at, ended_at, embedding,
                 embedding_model, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, vector(?), ?, ?, ?)
            """, (
                row['id'],
                row['workspace_path'],
                row['project_name'],
                row['summary'],
                row['accomplished'],
                row['decisions'],
                row['gotchas'],
                row['tags'],
                row['key_files'],
                row['started_at'],
                row['ended_at'],
                vector_str,
                row['embedding_model'],
                row['created_at'],
                row['updated_at']
            ))
            migrated += 1
            summary_preview = (row['summary'] or '')[:50]
            print(f"Migrated: {session_id} ({summary_preview}...)")
        except Exception as e:
            print(f"Failed to insert session {session_id}: {e}")

    turso.commit()

    # Verify
    result = turso.execute("SELECT COUNT(*) FROM sessions").fetchone()
    print(f"\nMigration complete: {migrated} sessions migrated")
    print(f"Turso sessions table now has {result[0]} rows")

    local.close()
    turso.close()
    return 0

if __name__ == '__main__':
    exit(main())
