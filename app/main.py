"""archive-stt: lesson markup (chapters and the transcript's paragraphs on the lesson's time line).

    POST /markup?uid=<kmedia unit>&language=he&force=false   start a run -> 202 + its state   (X-API-Key if keys are set, else from this host only)
    GET  /markup/{uid}         the markup document, public; 404 until a run is ready
    GET  /markup/{uid}/status  the last run's state, public
    GET  /health
"""
import logging
import threading
from contextlib import asynccontextmanager
from typing import Optional

from fastapi import FastAPI, Header, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse, JSONResponse

from . import config, kmedia, pipeline, store

logging.basicConfig(level=logging.INFO, format="%(levelname)s: %(name)s: %(message)s")
log = logging.getLogger("archive-stt")
# the markup is public data of public lessons: any site may read it
PUBLIC = {"Cache-Control": "public, max-age=300"}


@asynccontextmanager
async def lifespan(_app: FastAPI):
    stop = threading.Event()

    def tick():
        while not stop.wait(config.TICK_S):
            pipeline.sync_all()

    thread = threading.Thread(target=tick, name="sync", daemon=True)
    thread.start()
    yield
    stop.set()


app = FastAPI(title="archive-stt", lifespan=lifespan)
app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["GET", "POST"], allow_headers=["*"])


def _uid(uid: str) -> str:
    uid = (uid or "").strip()
    if not kmedia.UID_RE.match(uid):
        raise HTTPException(400, "uid: a kmedia content unit id (8 letters and digits)")
    return uid


LOOPBACK = ("127.0.0.1", "::1")


def _may_start(key: Optional[str], client: Optional[str]) -> None:
    """With keys set: a key, from anywhere. Without: only from this host (the script next to the service) --
    reading goes through the proxy, starting a run does not"""
    if config.API_KEYS:
        if key not in config.API_KEYS.values():
            raise HTTPException(401, "X-API-Key required")
    elif client not in LOOPBACK:
        raise HTTPException(403, "runs are started from the service's own host")


@app.post("/markup", status_code=202)
def start_markup(request: Request, uid: str, language: str = "he", force: bool = False,
                 x_api_key: Optional[str] = Header(None)):
    _may_start(x_api_key, request.client.host if request.client else None)
    uid = _uid(uid)
    if language not in config.LANGUAGES:
        raise HTTPException(400, f"language: one of {', '.join(config.LANGUAGES)}")
    try:
        return pipeline.start(uid, language, force)
    except pipeline.StartError as e:
        raise HTTPException(e.status, str(e))


@app.get("/markup/{uid}")
def get_markup(uid: str):
    uid = _uid(uid)
    pipeline.sync(uid)
    path = store.document_path(uid)
    if path is None:
        raise HTTPException(404, "no markup for this lesson", headers=PUBLIC)
    return FileResponse(path, media_type="application/json", headers=PUBLIC)


@app.get("/markup/{uid}/status")
def get_status(uid: str):
    uid = _uid(uid)
    state = pipeline.sync(uid)
    if state is None:
        raise HTTPException(404, "no markup run for this lesson", headers=PUBLIC)
    return JSONResponse(state, headers=PUBLIC)


@app.get("/health")
def health():
    return {"ok": True, "runs": len(store.uids_with_state())}
