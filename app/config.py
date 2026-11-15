"""Settings from the environment (see .env.example)"""
import os

# ai-trns, over HTTP: it transcribes the lesson's audio and labels the speakers; nothing else is taken from it
AI_TRNS_URL = os.environ.get("ARCHIVE_STT_AI_TRNS_URL", "http://127.0.0.1:8766").rstrip("/")
AI_TRNS_KEY = os.environ.get("ARCHIVE_STT_AI_TRNS_KEY", "")  # a service key of ai-trns (X-API-Key)
AI_TRNS_TIMEOUT = float(os.environ.get("ARCHIVE_STT_AI_TRNS_TIMEOUT", "60"))

# kmedia: the lesson's files by uid, the transcript's HTML (archive-unzip doc2html), the audio on the CDN
KMEDIA_URL = os.environ.get("ARCHIVE_STT_KMEDIA_URL", "https://kabbalahmedia.info").rstrip("/")
CDN_URL = os.environ.get("ARCHIVE_STT_CDN_URL", "https://cdn.kabbalahmedia.info").rstrip("/")
KMEDIA_TIMEOUT = float(os.environ.get("ARCHIVE_STT_KMEDIA_TIMEOUT", "30"))

# runs, ai-trns's answers and the documents; ai-trns deletes its jobs after a few days, these stay
DATA_DIR = os.environ.get("ARCHIVE_STT_DATA_DIR", "./data")

# keys allowed to start a run (POST /markup), "name:key,..."; empty = anyone may (reading is always public)
API_KEYS = dict(x.split(":", 1) for x in os.environ.get("ARCHIVE_STT_API_KEYS", "").split(",") if ":" in x)

TICK_S = float(os.environ.get("ARCHIVE_STT_TICK_S", "10"))  # how often pending runs are moved on
LANGUAGES = ("he", "en", "ru")
