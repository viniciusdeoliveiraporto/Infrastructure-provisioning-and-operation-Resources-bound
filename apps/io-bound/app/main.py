from fastapi import FastAPI, File, HTTPException, UploadFile
from fastapi.responses import Response
from app.file_service import save_file, read_file, list_files

app = FastAPI(
    title="IO-Bound File API",
    description="An API for experimenting with file I/O performance.",
    version="0.1.0",
)

@app.get("/health")
async def health_check():
    return {"status": "ok"}

@app.post("/files", status_code=201)
async def upload_file(file: UploadFile = File(...)):
    try:
        result = await save_file(file)
        return {
            "message": "File uploaded successfully",
            "file": result,
        }

    except Exception as exc:
        raise HTTPException(
            status_code=500,
            detail="Failed to save the uploaded file.",
        ) from exc

@app.get("/files/{file_id}/content")
async def get_file_content(file_id: str):
    content = await read_file(file_id)
    return Response(
        content=content,
        media_type="application/octet-stream",
    )

@app.get("/files")
async def get_files():
    files = await list_files()

    return {
        "total": len(files),
        "files": files,
    }