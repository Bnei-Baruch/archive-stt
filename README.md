# archive-stt

Разметка урока: главы (чтение, объяснение, вопрос-ответ, прочее) и абзацы транскрипта на временной оси урока.
План и модель: `work-strategy/pitches/lesson-markup.md`.

## Разделение с ai-trns
- **ai-trns** (GPU, только по HTTP): слова с таймкодами и спикеры. Больше ничего отсюда не берётся.
  Две задачи на урок: `POST /jobs` (слова → SRT) и `POST /diarize/words` (SRT + `uid` → `?format=paragraphs`:
  абзацы со словами, временем, спикером `הרב / קריין / תלמיד` и флагом `reading`).
- **archive-stt** (здесь): файлы урока из kmedia, цепочка задач, сборка разметки (`app/build.py`),
  хранение и отдача. Ответ ai-trns сохраняется сразу (`data/stt/`): задачи там удаляются через несколько дней,
  а разметку можно пересобрать без GPU.

## API
| | | |
|---|---|---|
| `POST` | `/markup?uid=&language=he&force=false` | запуск, `202` + состояние. С `ARCHIVE_STT_API_KEYS` нужен `X-API-Key`, без них — только с самого сервера (`127.0.0.1`) |
| `GET` | `/markup/{uid}` | документ разметки, публично (CORS `*`); 404, пока нет готового запуска |
| `GET` | `/markup/{uid}/status` | последний запуск: `pending` (`stage`: `words` → `speakers`) → `ready` / `failed` |
| `GET` | `/health` | |

Хранилище (`ARCHIVE_STT_DATA_DIR`): `state/`, `stt/<uid>/<run>.json`, `runs/<uid>/<run>.json`, `markup/<uid>.json`.
Недоступность ai-trns не валит запуск: следующая попытка через `ARCHIVE_STT_TICK_S`.

## Запуск
```bash
python3 -m venv .venv && .venv/bin/pip install -r requirements.txt
.venv/bin/python -m pytest
set -a; . ./.env; set +a; .venv/bin/uvicorn app.main:app --port 8770
```
Настройки: `.env.example`.

## Деплой
GitHub Actions → CI/CD → Run workflow (`.github/workflows/ci-cd.yml`): тесты, код в `/home/archive-stt/app` на хосте
ai-tools (рядом с ai-trns), `pip install`, рестарт systemd `archive-stt`, проверка `/health`. Первый запуск сам заводит
пользователя `archive-stt`, venv `/home/archive-stt/env` и каталог данных. Настройки (`/home/archive-stt/archive-stt.env`)
пишутся из секретов (`ARCHIVE_STT_AI_TRNS_URL`, `ARCHIVE_STT_AI_TRNS_KEY`, `ARCHIVE_STT_API_KEYS`) и переменных репо
(`ARCHIVE_STT_DATA_DIR`, `ARCHIVE_STT_KMEDIA_URL`, `ARCHIVE_STT_CDN_URL`, `ARCHIVE_STT_PORT`).
SSH — как у ai-trns: секреты `DEPLOY_HOST`, `DEPLOY_JUMP_HOST`, `DEPLOY_KNOWN_HOSTS`, `DEPLOY_SSH_KEY`.
