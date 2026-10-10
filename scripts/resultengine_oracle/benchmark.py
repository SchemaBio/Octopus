"""Run on Linux in the legacy query container; entirely synthetic fixtures."""
import argparse
import hashlib
import json
import os
import resource
import sys
import time
from pathlib import Path

sys.path.insert(0, '/srv')
import duckdb
import server

# COPY can print an interactive progress bar into stdout, including after JSON.
# Keep the benchmark's machine-readable output separate from that UI.
original_connect = duckdb.connect
def quiet_connect(*args, **kwargs):
    conn = original_connect(*args, **kwargs)
    conn.execute('SET enable_progress_bar=false')
    conn.execute("SET temp_directory='/tmp/resultengine-benchmark/spill'")
    return conn
duckdb.connect = quiet_connect

p=argparse.ArgumentParser(); p.add_argument('--root',default='/tmp/resultengine-benchmark'); p.add_argument('--rows',type=int,default=100000); p.add_argument('--operation',choices=['generate','query','sort','filter','export','prepare'],default='generate'); args=p.parse_args()
root=Path(args.root); root.mkdir(parents=True,exist_ok=True); file=root/(str(args.rows)+'.parquet')
server.ROOT=root.resolve(); server.ASSESSMENT_ROOT=root/'assessments'
if args.operation=='generate':
    conn=duckdb.connect(); conn.execute("SET memory_limit='256MB'"); conn.execute('SET threads=1')
    conn.execute("COPY (SELECT 'chr'||CAST(i%22+1 AS VARCHAR) AS Chromosome, CAST(i*13 AS VARCHAR) AS Position, 'GENE'||CAST(i%5000 AS VARCHAR) AS Gene, 'SNP' AS Type, 'missense_variant' AS Consequence, 'ENST000001' AS Transcript, CAST((i%1000)/1000.0 AS VARCHAR) AS AlphaMissense_AM, CASE WHEN i%7=0 THEN '.' ELSE CAST((i%100)/100000.0 AS VARCHAR)||'&'||CAST((i%10)/1000000.0 AS VARCHAR) END AS GnomAD_AF, repeat(md5(CAST(i AS VARCHAR)),4) AS Note FROM range("+str(args.rows)+") t(i)) TO ? (FORMAT PARQUET,COMPRESSION 'SNAPPY',ROW_GROUP_SIZE 65536)",[str(file)])
    print(json.dumps({'rows':args.rows,'bytes':file.stat().st_size,'sha256':hashlib.sha256(file.read_bytes()).hexdigest()})); sys.exit()
request={'table':'snv-indel','filePath':str(file),'datasetId':'a'*64,'objectSha256':hashlib.sha256(file.read_bytes()).hexdigest(),'limit':20,'offset':0}
if args.operation in ['sort','export']: request.update(sort='GnomAD_AF',direction='asc')
if args.operation=='filter': request['filters']=[{'column':'GnomAD_AF','operator':'lt','value':0.000002}]
start=time.perf_counter()
if args.operation=='prepare':
    server.ASSESSMENT_ROOT.mkdir(parents=True,exist_ok=True)
    for old in server.ASSESSMENT_ROOT.glob(request['datasetId']+'-'+request['objectSha256']+'-*'):
        old.unlink()
    result=server.prepare_automatic_acmg(request); total=result['rows']
elif args.operation=='export': result=server.execute(request,export=True); total=result.stat().st_size; result.unlink()
else: total=server.execute(request)['total']
print(json.dumps({'operation':args.operation,'rows':args.rows,'elapsedSeconds':time.perf_counter()-start,'peakRSSKiB':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss,'result':total,'duckdb':duckdb.__version__}))
