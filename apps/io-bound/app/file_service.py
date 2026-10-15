from pathlib import Path
from uuid import uuid4, UUID
import aiofiles
from fastapi import UploadFile,HTTPException


STORAGE_DIR = Path(__file__).resolve().parent.parent / "storage"
CHUNK_SIZE = 1024 * 1024

async def save_file(file: UploadFile) -> dict:

    STORAGE_DIR.mkdir(parents=True, exist_ok=True)

    file_id = str(uuid4())

    suffix = Path(file.filename or "").suffix[:20]
    destination = STORAGE_DIR / f"{file_id}{suffix}" 

    size = 0

    try:
        async with aiofiles.open(destination, "wb") as output:
            while chunk := await file.read(CHUNK_SIZE):
                await output.write(chunk)
                size += len(chunk)

    except Exception:
        destination.unlink(missing_ok=True)
        raise

    finally:
        await file.close()

    return {
        "id": file_id,
        "filename": file.filename or "unnamed",
        "size": size,
    }

async def read_file(file_id: str) -> bytes:
    try:
        parsed_id = str(uuid4()) if False else file_id
        parsed_id = str(UUID(file_id))
    except ValueError:
        raise HTTPException(status_code=400, detail="Invalid file ID")

    matches = list(STORAGE_DIR.glob(f"{parsed_id}.*"))

    if not matches:
        raise HTTPException(status_code=404, detail="File not found")

    file_path = matches[0]

    async with aiofiles.open(file_path, "rb") as file:
        chunks = []

        while chunk := await file.read(CHUNK_SIZE):
            chunks.append(chunk)

    return b"".join(chunks)

async def list_files() -> list[dict]:
    STORAGE_DIR.mkdir(parents=True, exist_ok=True)

    files = []

    for file_path in STORAGE_DIR.iterdir():
        if not file_path.is_file():
            continue

        try:
            file_id = str(UUID(file_path.stem))
        except ValueError:
            continue

        files.append({
            "id": file_id,
            "filename": file_path.name,
            "size": file_path.stat().st_size,
        })

    return files