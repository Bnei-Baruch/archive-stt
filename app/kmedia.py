"""kmedia: a lesson's files by uid and its transcript as kmedia shows it (archive-unzip doc2html)"""
import re
from typing import Optional

import httpx

from . import config

UID_RE = re.compile(r"^[A-Za-z0-9]{8}$")
TRANSCRIPT_TYPES = ("tamlil", "", None)


def fetch_unit(uid: str) -> dict:
    """The content unit with its files; LookupError if kmedia has no such unit"""
    res = httpx.get(f"{config.KMEDIA_URL}/backend/content_units/{uid}", timeout=config.KMEDIA_TIMEOUT)
    if res.status_code == 404:
        raise LookupError(uid)
    res.raise_for_status()
    return res.json()


def fetch_transcript_html(file_id: str) -> str:
    res = httpx.get(f"{config.KMEDIA_URL}/assets/api/doc2html/{file_id}", timeout=60,
                    headers={"User-Agent": "archive-stt"})
    res.raise_for_status()
    return res.text


def audio_url(file_id: str) -> str:
    return f"{config.CDN_URL}/{file_id}"


def lesson_files(unit: dict, language: str) -> tuple[Optional[dict], Optional[dict]]:
    """The unit's audio in `language` (the original recording -- "_o_" in its name -- before a translation) and its
    transcript in that language (a text file, not a PDF), or None"""
    files = unit.get("files") or []
    audio = [f for f in files if f.get("type") == "audio" and f.get("language") == language
             and f.get("mimetype", "audio/mpeg") == "audio/mpeg"]
    audio.sort(key=lambda f: "_o_" not in (f.get("name") or ""))
    texts = [f for f in files if f.get("type") == "text" and f.get("language") == language
             and f.get("insert_type") in TRANSCRIPT_TYPES and not (f.get("name") or "").lower().endswith(".pdf")]
    texts.sort(key=lambda f: not (f.get("name") or "").lower().endswith(".docx"))
    return (audio[0] if audio else None), (texts[0] if texts else None)
