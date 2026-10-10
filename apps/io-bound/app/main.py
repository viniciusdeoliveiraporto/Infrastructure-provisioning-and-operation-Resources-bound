from fastapi import FastAPI, File, HTTPException, UploadFile

from app.file_service import save_file


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