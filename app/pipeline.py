"""A run: the lesson's audio -> ai-trns words job -> ai-trns speakers job -> markup built here and stored.
Driven by polling (sync): on every read and by the maintenance thread (sync_all). One run per lesson at a time."""
import json
import logging
import threading
import uuid
from datetime import datetime, timezone
from typing import Optional

from . import aitrns, build, config, kmedia, store

log = logging.getLogger("archive-stt")
lock = threading.RLock()


class StartError(Exception):
    def __init__(self, status: int, message: str):
        super().__init__(message)
        self.status = status


def now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def _job_done(job_id: Optional[str]) -> tuple[Optional[dict], Optional[str]]:
    """(the finished job, None), (None, None) while it runs, (None, error) if it failed"""
    j = aitrns.job(job_id) if job_id else None
    if j is None:
        return None, "job is lost"
    if j["status"] not in aitrns.FINAL:
        return None, None
    if j["status"] != aitrns.DONE:
        return None, j.get("error") or j["status"]
    return j, None


def _build(state: dict, paragraphs_result: dict) -> dict:
    html = None
    if state.get("transcript"):
        try:
            html = kmedia.fetch_transcript_html(state["transcript"]["file_uid"])
        except Exception as e:
            log.warning("markup %s: no transcript (%s), built from the audio", state["uid"], e)
    run = {"id": state["run_id"], "created": now(),
           "jobs": {"words": state["words_job"], "speakers": state["speakers_job"]}}
    return build.markup(state["uid"], state["language"], state["media"], paragraphs_result["paragraphs"], html,
                        state.get("transcript"), run)


def _advance(state: dict) -> dict:
    if state["stage"] == "words":
        j, error = _job_done(state.get("words_job"))
        if error:
            return {**state, "status": "failed", "error": f"transcription: {error}"}
        if j is None:
            return state
        srt = aitrns.result(j["id"], "srt")
        job_id = aitrns.create_speakers_job(state["audio_url"], state["language"], srt, state["uid"])
        return {**state, "stage": "speakers", "speakers_job": job_id}
    j, error = _job_done(state.get("speakers_job"))
    if error:
        return {**state, "status": "failed", "error": f"speakers: {error}"}
    if j is None:
        return state
    answer = json.loads(aitrns.result(j["id"], "paragraphs"))
    store.save_stt(state["uid"], state["run_id"], answer)  # first: the markup can be rebuilt from it
    doc = _build(state, answer)
    store.save_document(doc)
    return {**state, "status": "ready", "finished": now(), "stats": {**doc["stats"], **answer.get("stats", {})}}


def sync(uid: str) -> Optional[dict]:
    """The run's state, moved on if its current job has finished"""
    state = store.load_state(uid)
    if state is None or state.get("status") != "pending":
        return state
    with lock:
        state = store.load_state(uid)
        if state.get("status") != "pending":
            return state
        try:
            new = _advance(state)
        except aitrns.AiTrnsError as e:  # ai-trns is down for a moment: try again at the next tick
            log.warning("markup %s: %s", uid, e)
            return state
        except Exception as e:
            log.exception("markup %s: failed", uid)
            new = {**state, "status": "failed", "error": f"{type(e).__name__}: {e}"}
        if new != state:
            store.save_state(new)
            log.info("markup %s: %s %s", uid, new["status"], new.get("stage"))
        return new


def sync_all() -> None:
    for uid in store.uids_with_state():
        try:
            sync(uid)
        except Exception:
            log.exception("markup %s: sync failed", uid)


def start(uid: str, language: str, force: bool) -> dict:
    with lock:
        state = sync(uid)
        if state and state["status"] == "pending" and not force:
            return state
        try:
            unit = kmedia.fetch_unit(uid)
        except LookupError:
            raise StartError(404, f"no kmedia unit {uid}")
        except Exception as e:
            raise StartError(502, f"kmedia: {e}")
        audio, transcript = kmedia.lesson_files(unit, language)
        if audio is None:
            raise StartError(422, f"unit {uid} has no {language} audio")
        url = kmedia.audio_url(audio["id"])
        try:
            job_id = aitrns.create_words_job(url, language)
        except aitrns.AiTrnsError as e:
            raise StartError(502, str(e))
        state = {"uid": uid, "language": language, "run_id": uuid.uuid4().hex[:12], "status": "pending",
                 "stage": "words", "created": now(), "audio_url": url,
                 "media": {"file_uid": audio["id"], "duration_s": audio.get("duration") or unit.get("duration")},
                 "transcript": {"file_uid": transcript["id"]} if transcript else None,
                 "words_job": job_id}
        store.save_state(state)
    log.info("markup %s: started (ai-trns job %s)", uid, job_id)
    return state
