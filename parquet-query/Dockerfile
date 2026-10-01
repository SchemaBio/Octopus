FROM python:3.12-slim
WORKDIR /srv
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY server.py ./
ENV PARQUET_CACHE_ROOT=/data/parquet-cache PORT=9100 PYTHONUNBUFFERED=1
USER 10001:10001
EXPOSE 9100
CMD ["python", "server.py"]
