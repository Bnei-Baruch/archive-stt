"""Files under ARCHIVE_STT_DATA_DIR, never expired:
  state/{uid}.json             the last run: status pending -> ready | failed, stage words -> speakers
  stt/{uid}/{run_id}.json      what ai-trns answered (paragraphs with words, times, speakers) -- the input of a rebuild
  runs/{uid}/{run_id}.json     every finished run's markup document, never changed
  markup/{uid}.json            the current document (a copy of the last ready run)"""
import json
import os
from typing import Optional

from . import config


def path(*parts: str) -> str:
    return os.path.join(config.DATA_DIR, *parts)


def read(p: str):
    with open(p, encoding="utf-8") as f:
        return json.load(f)


def write(p: str, data) -> None:
    os.makedirs(os.path.dirname(p), exist_ok=True)
    with open(p + ".tmp", "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False)
    os.replace(p + ".tmp", p)


def load_state(uid: str) -> Optional[dict]:
    try:
        return read(path("state", f"{uid}.json"))
    except FileNotFoundError:
        return None


def save_state(state: dict) -> None:
    write(path("state", f"{state['uid']}.json"), state)


def uids_with_state() -> list[str]:
    try:
        return [n[:-5] for n in os.listdir(path("state")) if n.endswith(".json")]
    except FileNotFoundError:
        return []


def save_stt(uid: str, run_id: str, paragraphs_result: dict) -> None:
    write(path("stt", uid, f"{run_id}.json"), paragraphs_result)


def save_document(doc: dict) -> None:
    write(path("runs", doc["uid"], f"{doc['run']['id']}.json"), doc)
    write(path("markup", f"{doc['uid']}.json"), doc)


def document_path(uid: str) -> Optional[str]:
    p = path("markup", f"{uid}.json")
    return p if os.path.exists(p) else None
