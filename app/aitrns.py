"""ai-trns over HTTP. Two jobs per lesson, nothing else is used:
  1. POST /jobs            the audio's words (result as SRT: the cues the next job is aligned to)
  2. POST /diarize/words   the same audio + those cues + the lesson's uid (ai-trns finds the sources itself) ->
                           GET /jobs/{id}/result?format=paragraphs: [{speaker, label, reading, start, end, text,
                           words: [{word, start, end}]}] -- the time codes and the speakers.
ai-trns deletes a finished job after a few days, so the answer is saved here (store.py) as soon as it is ready."""
import httpx

from . import config

DONE, FINAL = "done", ("done", "error", "cancelled")


class AiTrnsError(Exception):
    pass


def _call(method: str, path: str, **kw) -> httpx.Response:
    headers = {"X-API-Key": config.AI_TRNS_KEY} if config.AI_TRNS_KEY else {}
    try:
        res = httpx.request(method, config.AI_TRNS_URL + path, headers=headers, timeout=config.AI_TRNS_TIMEOUT, **kw)
    except httpx.HTTPError as e:
        raise AiTrnsError(f"ai-trns: {type(e).__name__}: {e}") from e
    if res.status_code >= 400 and res.status_code != 404:
        raise AiTrnsError(f"ai-trns {method} {path}: {res.status_code} {res.text[:200]}")
    return res


def create_words_job(url: str, language: str) -> str:
    res = _call("POST", "/jobs", data={"url": url, "lang": language, "format": "srt", "fix_rtl": "false"})
    return res.json()["id"]


def create_speakers_job(url: str, language: str, srt: str, uid: str) -> str:
    res = _call("POST", "/diarize/words", data={"url": url, "lang": language, "paragraphs": "true", "uid": uid},
                files={"srt": ("cues.srt", srt.encode("utf-8"), "application/x-subrip")})
    return res.json()["id"]


def job(job_id: str):
    """The job, or None if ai-trns no longer has it"""
    res = _call("GET", f"/jobs/{job_id}")
    return None if res.status_code == 404 else res.json()


def result(job_id: str, fmt: str) -> str:
    res = _call("GET", f"/jobs/{job_id}/result", params={"format": fmt, "fix_rtl": "false"})
    if res.status_code == 404:
        raise AiTrnsError(f"ai-trns: no result {fmt} of job {job_id}")
    return res.text
