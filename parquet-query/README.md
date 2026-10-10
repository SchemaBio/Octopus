# Development reference only

This directory preserves the frozen Python query implementation, SVC adapter,
pinned upstream source and tests as compatibility oracles. It is excluded from
the Docker build context and has no runtime Dockerfile or Compose service.

Run with Python 3.12 and the pinned development requirements:

```sh
python -m pip install -r parquet-query/requirements.txt
PYTHONPATH=parquet-query:parquet-query/vendor/svcv4/src:parquet-query/vendor/svcv4 \
  python -X utf8 -m pytest parquet-query/vendor/svcv4/tests \
  parquet-query/test_server.py parquet-query/test_svcv4.py -q
python -X utf8 scripts/resultengine_oracle/generate.py
python -X utf8 scripts/resultengine_oracle/validation.py
python -X utf8 scripts/resultengine_oracle/parquet.py
go test ./internal/resultengine ./internal/svcv4
```

Reference revision: `ef66faff51a265fef7b5c4e6439905f3aa540c46`.
Never regenerate stored clinical assessments merely because the implementation
changes. Future rule revisions must be independent versioned rules.
