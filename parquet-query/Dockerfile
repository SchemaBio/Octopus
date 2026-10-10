FROM python:3.12-slim
WORKDIR /srv
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY server.py ./
COPY svcv4.py ./
COPY vendor/svcv4/src ./vendor/svcv4/src
COPY vendor/svcv4/schemas ./vendor/svcv4/schemas
COPY vendor/svcv4/LICENSE ./vendor/svcv4/LICENSE
ENV PARQUET_CACHE_ROOT=/data/parquet-cache PORT=9100 PYTHONUNBUFFERED=1
USER 10001:10001
EXPOSE 9100
CMD ["python", "server.py"]
