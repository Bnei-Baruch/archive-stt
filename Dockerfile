FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 \
    ARCHIVE_STT_DATA_DIR=/data ARCHIVE_STT_PORT=8770

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY app ./app

# the data dir is a volume; its owner on the host must match the user the container runs as (compose: user)
RUN useradd --system --uid 10001 --no-create-home archive-stt && mkdir -p /data && chown archive-stt: /data
USER archive-stt
VOLUME /data

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD python -c "import os, urllib.request; urllib.request.urlopen(f'http://127.0.0.1:{os.environ[\"ARCHIVE_STT_PORT\"]}/health', timeout=4)"

CMD ["sh", "-c", "exec uvicorn app.main:app --host 0.0.0.0 --port \"$ARCHIVE_STT_PORT\""]
