#!/usr/bin/env python3
"""
Minimal sandbox runtime HTTP server.

Implements the same API as kubernetes-sigs/agent-sandbox python-runtime-sandbox:
  GET  /              -> health check
  POST /execute       -> {"command": "..."} -> {"stdout": "...", "stderr": "...", "exit_code": N}
  POST /upload        -> multipart file upload -> {"message": "File '...' uploaded successfully."}
  GET  /files/:name   -> file content download
  POST /files/read    -> {"path": "..."} -> file content
  POST /files/write   -> {"path": "...", "content": "..."} -> {"message": "..."}
"""

import os
import json
import subprocess
import shutil
from pathlib import Path
from fastapi import FastAPI, UploadFile, File, Request, HTTPException
from fastapi.responses import PlainTextResponse, JSONResponse
import uvicorn

app = FastAPI(title="foxctl Agent Sandbox Runtime")
WORKDIR = Path("/workspace")
WORKDIR.mkdir(exist_ok=True)

_repo_provisioned = False


def ensure_repo():
    """Clone the repo on first command if GIT_REPO_URL is set."""
    global _repo_provisioned
    if _repo_provisioned or not os.environ.get("GIT_REPO_URL"):
        return
    try:
        result = subprocess.run(
            ["/usr/local/bin/repo-provision"],
            capture_output=True, text=True, timeout=120,
        )
        if result.returncode != 0:
            print(f"[repo-provision] failed: {result.stderr}", flush=True)
        else:
            print(f"[repo-provision] success", flush=True)
    except Exception as e:
        print(f"[repo-provision] error: {e}", flush=True)
    _repo_provisioned = True


@app.get("/")
async def health():
    return {"status": "ok", "message": "Sandbox Runtime is active."}


@app.post("/execute")
async def execute(request: Request):
    body = await request.json()
    command = body.get("command", "")
    if not command:
        raise HTTPException(status_code=400, detail="command is required")

    ensure_repo()

    try:
        result = subprocess.run(
            command,
            shell=True,
            cwd=str(WORKDIR),
            capture_output=True,
            text=True,
            timeout=600,  # 10 min max
        )
        return JSONResponse({
            "stdout": result.stdout,
            "stderr": result.stderr,
            "exit_code": result.returncode,
        })
    except subprocess.TimeoutExpired:
        return JSONResponse(
            {"stdout": "", "stderr": "command timed out", "exit_code": 124},
            status_code=200,
        )
    except Exception as e:
        return JSONResponse(
            {"stdout": "", "stderr": str(e), "exit_code": 1},
            status_code=200,
        )


@app.post("/upload")
async def upload_file(file: UploadFile = File(...)):
    dest = WORKDIR / file.filename
    content = await file.read()
    dest.write_bytes(content)
    return {"message": f"File '{file.filename}' uploaded successfully."}


@app.get("/files/{filename:path}")
async def read_file(filename: str):
    path = WORKDIR / filename
    if not path.exists():
        raise HTTPException(status_code=404, detail=f"file '{filename}' not found")
    return PlainTextResponse(path.read_text())


@app.post("/files/read")
async def read_file_json(request: Request):
    body = await request.json()
    path = body.get("path", "")
    full = WORKDIR / path
    if not full.exists():
        raise HTTPException(status_code=404, detail=f"file '{path}' not found")
    content = full.read_bytes()
    return JSONResponse(content=content)


@app.post("/files/write")
async def write_file_json(request: Request):
    body = await request.json()
    path = body.get("path", "")
    content = body.get("content", "")
    full = WORKDIR / path
    full.parent.mkdir(parents=True, exist_ok=True)
    if isinstance(content, bytes):
        full.write_bytes(content)
    else:
        full.write_text(content)
    return {"message": f"File '{path}' written successfully."}


@app.get("/files")
async def list_files():
    files = []
    for f in WORKDIR.rglob("*"):
        if f.is_file():
            files.append(str(f.relative_to(WORKDIR)))
    return {"files": files}


@app.delete("/files/{filename:path}")
async def delete_file(filename: str):
    path = WORKDIR / filename
    if path.exists():
        path.unlink()
        return {"message": f"File '{filename}' deleted."}
    raise HTTPException(status_code=404, detail=f"file '{filename}' not found")


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8888, log_level="warning")
