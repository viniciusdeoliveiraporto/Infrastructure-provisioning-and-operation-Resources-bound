from pathlib import Path
from uuid import uuid4
import aiofiles
from fastapi import UploadFile

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