# archive-stt

Разметка урока: главы (чтение, объяснение, вопрос-ответ, прочее) и абзацы транскрипта на временной оси урока.
Go, один статический бинарник; данные — SQLite и JSON-файлы.
По `uid` урока из kmedia сервис получает от ai-trns слова со временем и спикеров, собирает разметку,
хранит её и отдаёт публично — для kmedia и других клиентов.
План и модель: `work-strategy/pitches/lesson-markup.md`.

## Как устроено

```
скрипт на сервере / по SSH-туннелю                         kmedia, браузер, поиск
        │ POST /cus/{uid}/markup                                        │ GET (https)
        ▼                                                          ▼
archive-stt  (ai-tools :8770)  ◀──── только GET ──── Angie: https://ai-tools.kab.info/archive-stt/
   │  1. kmedia: файлы урока (аудио, транскрипт)
   │  2. ai-trns POST /jobs            → слова (SRT)
   │  3. ai-trns POST /diarize/words   → слова со временем (?format=json) + абзацы: спикер, reading (?format=paragraphs)
   │  4. сборка разметки (markup/), запись в ARCHIVE_STT_DATA_DIR
   ▼
ai-trns (ai-tools :8766, GPU)
```

- **ai-trns** (GPU, только по HTTP): слова с таймкодами и спикеры. Больше ничего оттуда не берётся.
  Две задачи на урок: `POST /jobs` (слова → SRT) и `POST /diarize/words` (SRT + `uid`). Результат второй берём
  в двух форматах: `?format=json` — слова, время каждого выровнено по аудио (CTC); `?format=paragraphs` — абзацы
  со спикером (`Rav` / `Reader` / `SPEAKER_NN`, подпись `הרב / קריין / תלמיד`) и флагом `reading`; источники
  урока ai-trns находит сам по `uid`.
- **archive-stt** (здесь): файлы урока из kmedia, цепочка задач, сборка разметки, хранение и отдача.
  Ответ ai-trns сохраняется сразу — слова (`words/`) и диаризация (таблица `diarization`: абзацы
  `?format=paragraphs` без текста и слов): там задачи удаляются через несколько дней, а разметку можно
  пересобрать без GPU. Слова абзаца — слова его отрезка времени; текст абзацев не хранится, документ без
  транскрипта получает его из слов при отдаче.
- Аудио — оригинальная запись урока (файл с `_o_` в имени); её язык — язык урока, если `language` не задан.
  Транскрипт — текстовый файл kmedia на том же языке (`tamlil` или без типа, не PDF), через archive-unzip `doc2html`.
- Запуск движется опросом: при каждом чтении и фоновым циклом раз в `server.tick` (10 с).
  Недоступность ai-trns не валит запуск — следующая попытка на следующем тике. Один запуск на урок одновременно.

| Код | |
|---|---|
| `main.go`, `cmd/` | команды (cobra) и настройки (viper): `server`, `config`, `health`, `version` |
| `api/` | HTTP API (`net/http`), права на запуск, CORS |
| `pipeline/` | запуск: kmedia → задача слов → задача спикеров → сборка → запись |
| `markup/` | разметка из абзацев ai-trns и (если есть) размеченного транскрипта; выравнивание (`textdiff.go`); документ |
| `aitrns/`, `kmedia/` | клиенты ai-trns и kmedia |
| `store/`, `migrations/` | SQLite и файлы слов; схема БД |
| `models/`, `common/` | типы данных и константы (роли, статусы) |

## Документ разметки

`GET /cus/{uid}/markup`:
```json
{
  "version": 2, "uid": "5JBUHZoo", "original_language": "he", "language": "he",
  "media":        {"file_uid": "1lb9IZdV", "duration_s": 4710},
  "transcript":   {"file_uid": "..."},
  "run":          {"id": "...", "created": "...", "words_source": "stt", "source": "transcript"},
  "segmentation": {"rev": 1, "source": "auto", "created": "..."},
  "paragraphs":   [{"i": 0, "start": 0.47, "end": 23.21, "speaker": "reader"}],
  "chapters":     [{"type": "reading", "title": "...", "start": 28.0, "end": 661.0, "p_start": 1, "p_end": 1}],
  "stats":        {}
}
```
- Время — секунды медиафайла `media.file_uid`.
- `original_language` — язык оригинальной записи урока (файл с `_o_` в имени); `language` — язык файла, по
  которому построена разметка (по умолчанию оригинал).
- `paragraphs[].i`, `chapters[].p_start / p_end` — индекс `<p>` в HTML транскрипта (`doc2html`, те абзацы, что
  показывает kmedia). Без транскрипта (`run.source = "audio"`, `transcript = null`) — индекс абзаца ai-trns,
  и тогда у абзаца есть `text`.
- `speaker` — код роли: `rav` / `reader` / `student` или `null`; подпись рисует клиент.
- `segmentation` — ревизия абзацев и глав. Текущая — последняя `human` / `migrated`, если есть, иначе последняя
  `auto`; `?rev=N` — конкретная. Каждый запуск пишет новую ревизию `auto`.
- Типы глав: `reading` (чтение источника; `title` — объявление чтеца как есть), `commentary` (Рав после чтения),
  `qa` (вопрос ученика, уточнения и ответы Рава; `title` — вопрос), `other` (начало, объявления).
  Неизвестный тип клиент показывает как `other`.
- С транскриптом главы берутся из его пометок (`קריין:`, `שאלה:`, `תלמיד:`); транскрипт без пометок не
  используется — разметка строится по аудио.
- Не размечается (намеренно): номер пункта источника, место в документе-источнике, семинары.

## API

| | | |
|---|---|---|
| `POST` | `/cus/{uid}/markup?language=&force=false` | запуск, `202` + запуск. `language` — по умолчанию язык оригинала. С `ARCHIVE_STT_API_KEYS` нужен `X-API-Key`, без них — только с самого сервера (`127.0.0.1`), иначе `403` |
| `GET` | `/cus/{uid}/markup?rev=` | документ разметки, публично (CORS `*`, кэш 5 мин); `404`, пока нет готового запуска |
| `GET` | `/cus/{uid}/runs` | запуски урока, новые первыми |
| `GET` | `/cus/{uid}/runs/latest`, `/cus/{uid}/runs/{id}` | запуск: `kind`, `file_uid`, `words_source`, `status` `pending` (`stage`: `words` → `speakers`) → `ready` / `failed` (`error`), задачи ai-trns |
| `GET` | `/health` | |

Ошибки запуска: `400` неверный `uid`/`language`, `404` нет такого урока в kmedia, `422` нет оригинальной записи,
язык не распознаётся или нет аудио на этом языке, `502` kmedia или ai-trns не ответили. `force=true` — запустить
заново, даже если идёт.

## Хранилище

`data.dir` (`ARCHIVE_STT_DATA_DIR`), ничего не удаляется:

| | |
|---|---|
| `archive-stt.db` | SQLite: `run` (запуски: файл, стадия, статус, задачи ai-trns), `diarization` (абзацы ai-trns запуска без текста: `speaker`, `label`, `reading`, `start`, `end`) и `segmentation` (ревизии абзацев и глав, без текста). Схема — `migrations/`, применяется при старте |
| `words/<file_uid>/<run>.json` | слова медиафайла: `{"words": [{word, start, end}]}` — неизменяемый слой; со `diarization` — вход для пересборки |

## Доступ

- **Чтение** — публично через Angie: `https://ai-tools.kab.info/archive-stt/cus/<uid>/markup`. Прокси пропускает
  только `GET` (`limit_except GET`), `POST` снаружи → `403`.
- **Запуск** — без ключей только с самого сервера: скриптом на ai-tools или по SSH-туннелю
  (`ssh -N -L 8771:127.0.0.1:8770 <сервер>`, затем `POST http://127.0.0.1:8771/cus/<uid>/markup`).
  Если когда-нибудь задать `ARCHIVE_STT_API_KEYS` (`имя:ключ,…`), запуск будет по `X-API-Key` откуда угодно —
  и с самого сервера тоже только с ключом.

## Настройки

`config.toml` в рабочем каталоге (`-c` — другой файл; шаблон — `archive-stt config`) и/или переменные окружения:
ключ `ai-trns.url` = `ARCHIVE_STT_AI_TRNS_URL`. На сервере — только переменные (`.env.example`).

| Ключ | по умолчанию | |
|---|---|---|
| `server.bind-address` | `:8770` | |
| `server.api-keys` | пусто | кто может запускать, `имя:ключ,…`; пусто — только с сервера |
| `server.tick` | `10s` | как часто двигать идущие запуски |
| `server.log-level` | `info` | `debug` / `info` / `warn` / `error` |
| `ai-trns.url` | `http://127.0.0.1:8766` | |
| `ai-trns.key` | | сервисный ключ ai-trns (его `AI_TRNS_API_KEYS`, имя `archive-stt`) |
| `ai-trns.timeout` | `60s` | |
| `kmedia.url` | `https://kabbalahmedia.info` | |
| `kmedia.cdn-url` | `https://cdn.kabbalahmedia.info` | аудио для ai-trns (хост должен быть в его `AI_TRNS_ALLOWED_URL_HOSTS`) |
| `kmedia.timeout` | `30s` | |
| `data.dir` | `./data` | хранилище |

## Локальный запуск

Go — версия из `go.mod` (`go 1.27.4`); более старый `go` сам скачает нужный toolchain.
```bash
make test                       # go vet + go test -race
go run . config > config.toml   # ключ ai-trns и т. п.
go run . server
curl -X POST 'http://127.0.0.1:8770/cus/5JBUHZoo/markup'
```
Нужен VPN (ai-trns во внутренней сети).

## Docker

Образ: статический бинарник на `distroless/static` (пользователь `nonroot`, 65532), проверка здоровья —
`archive-stt health`. Данные (SQLite и JSON) — в каталоге хоста, смонтированном в `/data`.
`network_mode: host`: ai-trns работает на хосте напрямую, `127.0.0.1:8766` из контейнера до него доходит,
а запуск без ключей со скрипта на хосте приходит с `127.0.0.1`, как ждёт сервис. Переменные compose
(в shell или в `.env` рядом с `docker-compose.yml`): `ARCHIVE_STT_ENV_FILE` — настройки сервиса (по умолчанию `.env`),
`ARCHIVE_STT_DATA_HOST` — каталог данных на хосте (`./data`), `ARCHIVE_STT_UID`/`GID` — его владелец (`65532`).

```bash
# на хосте ai-tools, вместо systemd-юнита (systemctl disable --now archive-stt)
ARCHIVE_STT_ENV_FILE=/home/archive-stt/archive-stt.env ARCHIVE_STT_DATA_HOST=/var/lib/archive-stt \
ARCHIVE_STT_UID=$(id -u archive-stt) ARCHIVE_STT_GID=$(id -g archive-stt) docker compose up -d --build
docker compose logs -f     # или journalctl -t archive-stt -f
```

## Сервер

Хост ai-tools (linux/amd64), рядом с ai-trns (адрес — у админов / в секрете `DEPLOY_HOST`):

| | |
|---|---|
| Бинарник | `/home/archive-stt/bin/archive-stt` (коммит — в `bin/DEPLOYED`) |
| Настройки | `/home/archive-stt/archive-stt.env` (600; ai-trns — `http://127.0.0.1:8766`) |
| Данные | `/var/lib/archive-stt` |
| Сервис | systemd `archive-stt` (`scripts/archive-stt.service`), `0.0.0.0:8770`; лог — `journalctl -u archive-stt` |
| Логи | stderr → journald (`journalctl -u archive-stt -f`); чистит сам journald по размеру (по умолчанию ≤ 10% диска, ≤ 4 ГБ на все сервисы). В Docker — тоже в journald (драйвер `journald`, `journalctl -t archive-stt`): переживают пересоздание контейнера при деплое. GET-запросы не логируются, кроме 5xx; уровень — `server.log-level` |
| Прокси | Angie на `ngx.kab.info`, `/etc/angie/conf.d/ai-tools.conf`, `location /archive-stt/` → `:8770`, только GET |

## Деплой

**GitHub Actions** → CI/CD → Run workflow (`.github/workflows/ci-cd.yml`): тесты, сборка linux/amd64, бинарник
в `/home/archive-stt/bin`, рестарт `archive-stt`, проверка `/health`. Первый запуск сам заводит пользователя
`archive-stt` и каталог данных. Настройки пишутся из секретов (`ARCHIVE_STT_AI_TRNS_URL`, `ARCHIVE_STT_AI_TRNS_KEY`,
`ARCHIVE_STT_API_KEYS`) и переменных репо (`ARCHIVE_STT_DATA_DIR`, `ARCHIVE_STT_KMEDIA_URL`, `ARCHIVE_STT_CDN_URL`,
`ARCHIVE_STT_PORT`). SSH — как у ai-trns: секреты `DEPLOY_HOST`, `DEPLOY_JUMP_HOST`, `DEPLOY_KNOWN_HOSTS`,
`DEPLOY_SSH_KEY` (пока не заведены — деплой вручную).

**Вручную**, пока нет секретов SSH (`HOST` — root@ хоста ai-tools):
```bash
HOST=root@<ai-tools>
make build-linux
ssh $HOST 'mkdir -p /home/archive-stt/bin; f=/home/archive-stt/bin/archive-stt; [ ! -f $f ] || cp -a $f $f.bak.$(date +%Y%m%d%H%M%S)'
scp archive-stt-linux-amd64 $HOST:/home/archive-stt/bin/archive-stt.new
scp scripts/archive-stt.service $HOST:/etc/systemd/system/archive-stt.service
ssh $HOST "mv /home/archive-stt/bin/archive-stt.new /home/archive-stt/bin/archive-stt \
  && echo '$(git rev-parse HEAD) manual' > /home/archive-stt/bin/DEPLOYED && chown -R archive-stt: /home/archive-stt/bin \
  && systemctl daemon-reload && systemctl restart archive-stt && sleep 2 && curl -sf localhost:8770/health"
```
В `archive-stt.env` — имена переменных из `.env.example` (`ARCHIVE_STT_SERVER_BIND_ADDRESS=:8770` вместо
`ARCHIVE_STT_PORT`, `ARCHIVE_STT_SERVER_API_KEYS`, `ARCHIVE_STT_KMEDIA_CDN_URL`).

## Массовый запуск

`archive-stt markup` — запуск разметки на сервере по списку uid, по `--parallel` штук одновременно (по умолчанию 1),
ожидание каждого до `ready` / `failed` и списки прошедших и упавших. Запускается с мака, нужен VPN.

```bash
go build -o archive-stt .
./archive-stt markup 5JBUHZoo                    # один урок
./archive-stt markup clips.txt                   # файл: uid в строке (после TAB — что угодно, # — комментарий)
... | ./archive-stt markup -                     # stdin
./archive-stt markup --dry-run clips.txt         # что будет запущено; ничего не запускает и не пишет
./archive-stt markup markup-failed.txt           # повторить упавшие
```

**Туннель.** Сервис принимает запуск только с самого сервера, поэтому команда сама открывает
`ssh -N -L 8771:127.0.0.1:8770 <сервер>` и закрывает его в конце. Сервер — `--ssh root@<ai-tools>`, или `markup.ssh`
в `config.toml`, или `ARCHIVE_STT_MARKUP_SSH`. `--no-ssh --url http://127.0.0.1:8770` — без туннеля (на самом сервере
или с локальным сервисом).

**Проверки перед запуском:**
1. uid есть в `markup-ok.txt` → пропуск, без запросов к серверу.
2. Сервер: разметка уже готова → в `markup-ok.txt`, не запускается; запуск уже идёт → ждём его.
3. Ответ на запуск: `404` (нет такого юнита в kmedia), `422` (нет аудио), прочее → в `markup-failed.txt`.

`--force` отключает 1 и 2 и запускает заново.

**Списки** (в текущей папке, не в git): `markup-ok.txt` — прошедшие, uid в строке, дописывается;
`markup-failed.txt` — упавшие, `uid<TAB>ошибка`, uid уходит оттуда, когда позже проходит. В конце — прошедшие и
упавшие этого запуска и сколько всего в каждом списке; код выхода `1`, если что-то упало.

**Остановка.** Ctrl+C — новые не запускаются, начатые дожидаются. Второй Ctrl+C — выход сразу; начатые
доработают на сервере, следующий запуск их подхватит (проверка 2). Сетевая ошибка (VPN, туннель) не прерывает:
повтор через `--poll`.

| | |
|---|---|
| `--language` | язык аудио (по умолчанию — язык оригинала урока) |
| `--parallel 1` | одновременно на сервере (GPU общий с ai-trns и live-трансляцией) |
| `--poll 20s` | между проверками статуса |
| `--dry-run`, `--force` | только показать / заново, даже если готово |
| `--ssh`, `--port 8771` | туннель |
| `--no-ssh`, `--url` | без туннеля |
| `--ok`, `--failed` | другие файлы списков |

Список юнитов из MDB — пока скрипт `local/mdb_units.py` (не в git, описание — `local/README.md`).
