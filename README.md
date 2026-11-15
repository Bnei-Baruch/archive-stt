# archive-stt

Разметка урока: главы (чтение, объяснение, вопрос-ответ, прочее) и абзацы транскрипта на временной оси урока.
По `uid` урока из kmedia сервис получает от ai-trns слова со временем и спикеров, собирает разметку,
хранит её и отдаёт публично — для kmedia и других клиентов.
План и модель: `work-strategy/pitches/lesson-markup.md`.

## Как устроено

```
скрипт на сервере / по SSH-туннелю                         kmedia, браузер, поиск
        │ POST /markup?uid=                                        │ GET (https)
        ▼                                                          ▼
archive-stt  (ai-tools :8770)  ◀──── только GET ──── Angie: https://ai-tools.kab.info/archive-stt/
   │  1. kmedia: файлы урока (аудио, транскрипт)
   │  2. ai-trns POST /jobs            → слова (SRT)
   │  3. ai-trns POST /diarize/words   → абзацы: слова, время, спикер, флаг reading
   │  4. сборка разметки (app/build.py), запись в ARCHIVE_STT_DATA_DIR
   ▼
ai-trns (ai-tools :8766, GPU)
```

- **ai-trns** (GPU, только по HTTP): слова с таймкодами и спикеры. Больше ничего оттуда не берётся.
  Две задачи на урок: `POST /jobs` (слова → SRT) и `POST /diarize/words` (SRT + `uid` → `?format=paragraphs`:
  абзацы со словами, временем, спикером `הרב / קריין / תלמיד` и флагом `reading`; источники урока ai-trns
  находит сам по `uid`).
- **archive-stt** (здесь): файлы урока из kmedia, цепочка задач, сборка разметки, хранение и отдача.
  Ответ ai-trns сохраняется сразу (`stt/`): там задачи удаляются через несколько дней, а разметку можно
  пересобрать без GPU.
- Аудио — оригинальная запись урока на языке `language` (файл с `_o_` в имени предпочтительнее перевода).
  Транскрипт — текстовый файл kmedia на том же языке (`tamlil` или без типа, не PDF), через archive-unzip `doc2html`.
- Запуск движется опросом: при каждом чтении статуса и фоновым потоком раз в `ARCHIVE_STT_TICK_S` секунд.
  Недоступность ai-trns не валит запуск — следующая попытка на следующем тике. Один запуск на урок одновременно.

| Код | |
|---|---|
| `app/main.py` | API (FastAPI), права на запуск, фоновый поток |
| `app/pipeline.py` | запуск: kmedia → задача слов → задача спикеров → сборка → запись |
| `app/build.py` | разметка из абзацев ai-trns и (если есть) размеченного транскрипта; только stdlib |
| `app/textdiff.py` | выравнивание слов транскрипта по услышанным |
| `app/aitrns.py`, `app/kmedia.py` | клиенты ai-trns и kmedia |
| `app/store.py` | файлы в `ARCHIVE_STT_DATA_DIR` |

## Документ разметки

`GET /markup/{uid}`:
```json
{
  "version": 1, "uid": "5JBUHZoo", "language": "he",
  "media":      {"file_uid": "1lb9IZdV", "duration_s": 4710},
  "transcript": {"file_uid": "..."},
  "run":        {"id": "...", "created": "...", "jobs": {"words": "...", "speakers": "..."}, "source": "transcript"},
  "paragraphs": [{"i": 0, "start": 0.47, "end": 23.21, "speaker": "קריין"}],
  "chapters":   [{"type": "reading", "title": "...", "start": 28.0, "end": 661.0, "p_start": 1, "p_end": 1}],
  "stats":      {}
}
```
- Время — секунды медиафайла `media.file_uid`.
- `paragraphs[].i`, `chapters[].p_start / p_end` — индекс `<p>` в HTML транскрипта (`doc2html`, те абзацы, что
  показывает kmedia). Без транскрипта (`run.source = "audio"`, `transcript = null`) — индекс абзаца ai-trns,
  и тогда у абзаца есть `text`.
- `speaker`: `הרב` / `קריין` / `תלמיד` или `null`.
- Типы глав: `reading` (чтение источника; `title` — объявление чтеца как есть), `commentary` (Рав после чтения),
  `qa` (вопрос ученика, уточнения и ответы Рава; `title` — вопрос), `other` (начало, объявления).
  Неизвестный тип клиент показывает как `other`.
- С транскриптом главы берутся из его пометок (`קריין:`, `שאלה:`, `תלמיד:`); транскрипт без пометок не
  используется — разметка строится по аудио.
- Не размечается (намеренно): номер пункта источника, место в документе-источнике, семинары.

## API

| | | |
|---|---|---|
| `POST` | `/markup?uid=&language=he&force=false` | запуск, `202` + состояние. С `ARCHIVE_STT_API_KEYS` нужен `X-API-Key`, без них — только с самого сервера (`127.0.0.1`), иначе `403` |
| `GET` | `/markup/{uid}` | документ разметки, публично (CORS `*`, кэш 5 мин); `404`, пока нет готового запуска |
| `GET` | `/markup/{uid}/status` | последний запуск: `pending` (`stage`: `words` → `speakers`) → `ready` / `failed` (`error`) |
| `GET` | `/health` | |

Ошибки запуска: `400` неверный `uid`/`language`, `404` нет такого урока в kmedia, `422` нет аудио на этом языке,
`502` kmedia или ai-trns не ответили. `force=true` — запустить заново, даже если идёт или готов.

## Хранилище

`ARCHIVE_STT_DATA_DIR`, ничего не удаляется:

| | |
|---|---|
| `state/<uid>.json` | последний запуск: статус, стадия, задачи ai-trns, файлы урока |
| `stt/<uid>/<run>.json` | ответ ai-trns — вход для пересборки |
| `runs/<uid>/<run>.json` | документ каждого готового запуска, не меняется |
| `markup/<uid>.json` | текущий документ (копия последнего готового) |

## Доступ

- **Чтение** — публично через Angie: `https://ai-tools.kab.info/archive-stt/markup/<uid>`. Прокси пропускает
  только `GET` (`limit_except GET`), `POST` снаружи → `403`.
- **Запуск** — без ключей только с самого сервера: скриптом на ai-tools или по SSH-туннелю
  (`ssh -N -L 8771:127.0.0.1:8770 <сервер>`, затем `POST http://127.0.0.1:8771/markup?uid=…`).
  Если когда-нибудь задать `ARCHIVE_STT_API_KEYS` (`имя:ключ,…`), запуск будет по `X-API-Key` откуда угодно —
  и с самого сервера тоже только с ключом.

## Настройки

`.env.example`; все переменные с префиксом `ARCHIVE_STT_`:

| | по умолчанию | |
|---|---|---|
| `AI_TRNS_URL` | `http://127.0.0.1:8766` | ai-trns |
| `AI_TRNS_KEY` | | сервисный ключ ai-trns (его `AI_TRNS_API_KEYS`, имя `archive-stt`) |
| `AI_TRNS_TIMEOUT` | `60` | |
| `KMEDIA_URL` | `https://kabbalahmedia.info` | |
| `CDN_URL` | `https://cdn.kabbalahmedia.info` | аудио для ai-trns (хост должен быть в его `AI_TRNS_ALLOWED_URL_HOSTS`) |
| `DATA_DIR` | `./data` | хранилище |
| `API_KEYS` | пусто | кто может запускать; пусто — только с сервера |
| `TICK_S` | `10` | как часто двигать идущие запуски |
| `PORT` | | порт для systemd-юнита (uvicorn `--port`) |

## Локальный запуск

```bash
/opt/homebrew/bin/python3.12 -m venv .venv && .venv/bin/pip install -r requirements.txt
.venv/bin/python -m pytest
cp .env.example .env    # ключ ai-trns, DATA_DIR=./data
set -a; . ./.env; set +a; .venv/bin/uvicorn app.main:app --port 8770
curl -X POST 'http://127.0.0.1:8770/markup?uid=5JBUHZoo'
```
Нужен VPN (ai-trns во внутренней сети). На macOS Python из miniconda может не иметь права «Локальная сеть» —
`No route to host` к внутренним адресам при работающем curl; Homebrew Python работает.

## Сервер

Хост ai-tools, рядом с ai-trns (адрес — у админов / в секрете `DEPLOY_HOST`):

| | |
|---|---|
| Код | `/home/archive-stt/app` (коммит — в `DEPLOYED`) |
| venv | `/home/archive-stt/env` (Python 3.12) |
| Настройки | `/home/archive-stt/archive-stt.env` (600; ai-trns — `http://127.0.0.1:8766`) |
| Данные | `/var/lib/archive-stt` |
| Сервис | systemd `archive-stt` (`scripts/archive-stt.service`), `0.0.0.0:8770`; лог — `journalctl -u archive-stt` |
| Прокси | Angie на `ngx.kab.info`, `/etc/angie/conf.d/ai-tools.conf`, `location /archive-stt/` → `:8770`, только GET |

## Деплой

**GitHub Actions** → CI/CD → Run workflow (`.github/workflows/ci-cd.yml`): тесты, код в `/home/archive-stt/app`,
`pip install`, рестарт `archive-stt`, проверка `/health`. Первый запуск сам заводит пользователя `archive-stt`,
venv и каталог данных. Настройки пишутся из секретов (`ARCHIVE_STT_AI_TRNS_URL`, `ARCHIVE_STT_AI_TRNS_KEY`,
`ARCHIVE_STT_API_KEYS`) и переменных репо (`ARCHIVE_STT_DATA_DIR`, `ARCHIVE_STT_KMEDIA_URL`, `ARCHIVE_STT_CDN_URL`,
`ARCHIVE_STT_PORT`). SSH — как у ai-trns: секреты `DEPLOY_HOST`, `DEPLOY_JUMP_HOST`, `DEPLOY_KNOWN_HOSTS`,
`DEPLOY_SSH_KEY` (пока не заведены — деплой вручную).

**Вручную**, пока нет секретов SSH (`HOST` — root@ хоста ai-tools):
```bash
HOST=root@<ai-tools>
rm -rf /tmp/out && mkdir /tmp/out && git archive HEAD | tar -x -C /tmp/out
ssh $HOST 'cp -a /home/archive-stt/app /home/archive-stt/app.bak.$(date +%Y%m%d%H%M%S)'
rsync -a --delete --exclude __pycache__ /tmp/out/ $HOST:/home/archive-stt/app/
ssh $HOST "echo '$(git rev-parse HEAD) manual' > /home/archive-stt/app/DEPLOYED \
  && chown -R archive-stt: /home/archive-stt/app \
  && runuser -u archive-stt -- /home/archive-stt/env/bin/pip install -q -r /home/archive-stt/app/requirements.txt \
  && systemctl restart archive-stt && sleep 3 && curl -sf localhost:8770/health"
```

## Массовый запуск

Скрипты `local/` (не в git): список юнитов из MDB и запуск разметки по списку через SSH-туннель,
со списками прошедших и упавших. Описание — `local/README.md`.
