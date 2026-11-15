"""The run: kmedia files -> ai-trns words job -> ai-trns speakers job -> markup stored and served.
ai-trns and kmedia are replaced by fakes at the module boundary (app.aitrns, app.kmedia)."""
import json

import pytest
from fastapi.testclient import TestClient

from app import aitrns, build, config, kmedia, pipeline
from app.main import app
from tests.test_build import PARAS, spoken

UNIT = {"id": "Unit0001", "duration": 60, "files": [
    {"id": "AudioTr1", "type": "audio", "language": "he", "mimetype": "audio/mpeg", "name": "heb_t_x.mp3"},
    {"id": "AudioOr1", "type": "audio", "language": "he", "mimetype": "audio/mpeg", "name": "heb_o_x.mp3"},
    {"id": "TextPdf1", "type": "text", "language": "he", "insert_type": "tamlil", "name": "heb_o_x.pdf"},
    {"id": "TextDoc1", "type": "text", "language": "he", "insert_type": "tamlil", "name": "heb_o_x.doc"},
]}


def paragraphs_answer():
    """What ai-trns answers to ?format=paragraphs: a paragraph per run of one speaker"""
    out = []
    for w in spoken(PARAS, skip={1}):
        if out and out[-1]["label"] == w["label"]:
            out[-1]["words"].append({k: w[k] for k in ("word", "start", "end")})
        else:
            out.append({"speaker": w["label"], "label": w["label"], "reading": False, "start": w["start"],
                        "words": [{k: w[k] for k in ("word", "start", "end")}]})
    for p in out:
        p["end"] = p["words"][-1]["end"]
        p["text"] = " ".join(x["word"] for x in p["words"])
    return {"paragraphs": out, "stats": {"paragraphs": len(out)}}


class FakeAiTrns:
    def __init__(self):
        self.jobs, self.calls = {}, []

    def create_words_job(self, url, language):
        self.calls.append(("words", url, language))
        self.jobs["w1"] = {"id": "w1", "status": "running"}
        return "w1"

    def create_speakers_job(self, url, language, srt, uid):
        self.calls.append(("speakers", url, language, srt, uid))
        self.jobs["s1"] = {"id": "s1", "status": "running"}
        return "s1"

    def job(self, job_id):
        return self.jobs.get(job_id)

    def result(self, job_id, fmt):
        return {("w1", "srt"): "1\n00:00:00,000 --> 00:00:01,000\nשלום\n",
                ("s1", "paragraphs"): json.dumps(paragraphs_answer(), ensure_ascii=False)}[(job_id, fmt)]


@pytest.fixture
def env(tmp_path, monkeypatch):
    monkeypatch.setattr(config, "DATA_DIR", str(tmp_path))
    monkeypatch.setattr(config, "API_KEYS", {})
    fake = FakeAiTrns()
    for name in ("create_words_job", "create_speakers_job", "job", "result"):
        monkeypatch.setattr(aitrns, name, getattr(fake, name))

    def fetch_unit(uid):
        if uid != "Unit0001":
            raise LookupError(uid)
        return UNIT

    monkeypatch.setattr(kmedia, "fetch_unit", fetch_unit)
    monkeypatch.setattr(kmedia, "fetch_transcript_html", lambda file_id: "".join(f"<p>{p}</p>" for p in PARAS))
    return TestClient(app), fake


def test_the_chain_words_then_speakers_then_markup(env):
    client, fake = env
    assert client.get("/markup/Unit0001").status_code == 404
    r = client.post("/markup", params={"uid": "Unit0001"})
    assert r.status_code == 202, r.text
    state = r.json()
    assert (state["status"], state["stage"], state["media"]["file_uid"], state["transcript"]) == \
           ("pending", "words", "AudioOr1", {"file_uid": "TextDoc1"})
    assert fake.calls == [("words", "https://cdn.kabbalahmedia.info/AudioOr1", "he")]
    # a second start while it runs is the same run
    assert client.post("/markup", params={"uid": "Unit0001"}).json()["run_id"] == state["run_id"]

    # nothing changes while ai-trns works
    assert client.get("/markup/Unit0001/status").json()["stage"] == "words"
    fake.jobs["w1"]["status"] = "done"
    state = client.get("/markup/Unit0001/status").json()
    assert state["stage"] == "speakers" and state["speakers_job"] == "s1"
    assert fake.calls[1][0] == "speakers" and fake.calls[1][4] == "Unit0001" and "שלום" in fake.calls[1][3]

    fake.jobs["s1"]["status"] = "done"
    pipeline.sync_all()
    state = client.get("/markup/Unit0001/status").json()
    assert state["status"] == "ready", state

    r = client.get("/markup/Unit0001", headers={"Origin": "https://kabbalahmedia.info"})
    assert r.status_code == 200 and r.headers["access-control-allow-origin"] == "*"
    doc = r.json()
    assert doc["run"]["source"] == "transcript" and doc["run"]["id"] == state["run_id"]
    assert [c["type"] for c in doc["chapters"]] == ["other", "reading", "commentary", "qa", "reading", "commentary"]
    assert doc["stats"]["aligned_paragraphs"] == 13
    # ai-trns's answer is kept, so the markup can be rebuilt without it
    assert (build.RAV in json.dumps(json.load(open(f"{config.DATA_DIR}/stt/Unit0001/{state['run_id']}.json",
                                                   encoding="utf-8")), ensure_ascii=False))


def test_a_failed_job_fails_the_run_and_it_can_be_started_again(env):
    client, fake = env
    client.post("/markup", params={"uid": "Unit0001"})
    fake.jobs["w1"].update(status="error", error="boom")
    state = client.get("/markup/Unit0001/status").json()
    assert state["status"] == "failed" and "boom" in state["error"]
    assert client.post("/markup", params={"uid": "Unit0001"}).json()["status"] == "pending"


def test_ai_trns_down_for_a_moment_does_not_fail_the_run(env, monkeypatch):
    client, fake = env
    client.post("/markup", params={"uid": "Unit0001"})

    def down(job_id):
        raise aitrns.AiTrnsError("ai-trns: ConnectError")

    monkeypatch.setattr(aitrns, "job", down)
    assert client.get("/markup/Unit0001/status").json()["status"] == "pending"


def test_checks(env, monkeypatch):
    client, _ = env
    assert client.get("/markup/bad").status_code == 400
    assert client.post("/markup", params={"uid": "Missing1"}).status_code == 404
    assert client.post("/markup", params={"uid": "Unit0001", "language": "xx"}).status_code == 400
    monkeypatch.setattr(config, "API_KEYS", {"dudi": "secret"})
    assert client.post("/markup", params={"uid": "Unit0001"}).status_code == 401
    assert client.post("/markup", params={"uid": "Unit0001"}, headers={"X-API-Key": "secret"}).status_code == 202
    assert client.get("/markup/Unit0001/status").status_code == 200  # reading stays public
