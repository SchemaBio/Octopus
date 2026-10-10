"""Generate deidentified, deterministic query/export compatibility fixtures."""
import hashlib
import json
import os
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT/'parquet-query'))
import duckdb
import server


def main():
    target = ROOT/'internal/resultengine/testdata'
    target.mkdir(parents=True, exist_ok=True)
    server.ROOT = target.resolve(); server.ASSESSMENT_ROOT = target/'assessments'
    con = duckdb.connect(':memory:')
    con.execute('CREATE TABLE source(Chromosome VARCHAR, Position VARCHAR, Gene VARCHAR, Type VARCHAR, Consequence VARCHAR, Transcript VARCHAR, AlphaMissense_AM VARCHAR, GnomAD_AF VARCHAR, Note VARCHAR)')
    con.executemany('INSERT INTO source VALUES (?,?,?,?,?,?,?,?,?)',[
        ('chr2','10','GENE1','SNP','missense_variant','ENST000001','0.995','0.1&0.01','comma, quote" and\nnewline'),
        ('chr10','2','GENE2','SNP','missense_variant','ENST000001','0.15','0.2',None),
        ('chrX','6','GENE3','INDEL','missense_variant','ENST000001','0.999','0.3',''),
        ('chr1','7','GENE4','SNP','synonymous_variant','ENST000001','0.99','.','.'),
        ('chrMT','7','GENE5','SNP','missense_variant','ENST000001','0.792','bad&0.0','Ω测试'),
        ('contig','7','GENE6','SNP','missense_variant','ENST000001','0.1',None,'null'),
    ])
    for compression in ['snappy','gzip','zstd','uncompressed']:
        path=target/(compression+'.parquet')
        con.execute("COPY source TO ? (FORMAT PARQUET, COMPRESSION '"+compression+"')",[str(path)])
    con.execute('DELETE FROM source'); con.execute('COPY source TO ? (FORMAT PARQUET)',[str(target/'empty.parquet')])
    con.execute('CREATE TABLE numeric(Chromosome VARCHAR, Position BIGINT, VAF DOUBLE, Depth BIGINT, Missing VARCHAR)')
    con.execute("INSERT INTO numeric VALUES ('1',9007199254740993,0.125,100,NULL),('X',NULL,NULL,NULL,'.')")
    con.execute('COPY numeric TO ? (FORMAT PARQUET)',[str(target/'numeric.parquet')])
    con.execute('CREATE TABLE nonfinite(Position VARCHAR, GnomAD_AF VARCHAR)')
    con.executemany('INSERT INTO nonfinite VALUES (?,?)',[('1','NaN'),('2','inf'),('3','-inf'),('4','0'),('5','bad'),('6',None)])
    con.execute('COPY nonfinite TO ? (FORMAT PARQUET)',[str(target/'nonfinite.parquet')])
    con.execute("INSERT INTO source VALUES ('1','1','GENE1','SNP','missense_variant','ENST000001','1.0','0',NULL),('2','2','GENE2','SNP','missense_variant','ENST000001','0.0','0',NULL)")
    con.execute('COPY source TO ? (FORMAT PARQUET)',[str(target/'boundaries.parquet')])
    con.close()
    cases=[]
    def add(file='snappy.parquet',export=False,prepare=False,**updates):
        path=target/file; fingerprint=hashlib.sha256(path.read_bytes()).hexdigest()
        request={'table':'snv-indel','filePath':str(path.resolve()),'datasetId':'a'*64,'objectSha256':fingerprint,'offset':0,'limit':20,'overlays':[],'filters':[]}
        request.update(updates)
        entry={'operation':'prepare' if prepare else 'export' if export else 'query','input':{**request,'filePath':file}}
        try:
            if prepare:
                result=server.prepare_automatic_acmg(request)
                entry['output']={'profile':result['profile'],'rows':result['rows'],'records':[json.loads(line) for line in Path(result['assessmentFile']).read_text(encoding='utf-8').splitlines()]}
            elif export:
                result=server.execute(request,export=True); entry['output']=result.read_text(encoding='utf-8'); result.unlink()
            else: entry['output']=server.execute(request)
        except Exception as error: entry['error']={'type':type(error).__name__,'message':str(error)}
        cases.append(entry)
    for file in ['snappy.parquet','gzip.parquet','zstd.parquet','uncompressed.parquet','empty.parquet','numeric.parquet']: add(file); add(file,export=True)
    add(prepare=True); add('empty.parquet',prepare=True)
    add('boundaries.parquet'); add('boundaries.parquet',export=True); add('boundaries.parquet',prepare=True)
    for table in sorted(server.TABLES): add(table=table)
    for column in ['Chromosome','Position','GnomAD_AF','Gene']:
        for direction in ['asc','desc']: add(sort=column,direction=direction,offset=1,limit=3); add(sort=column,direction=direction,export=True)
    for operator,value in [('contains','gene'),('equals','GENE2'),('in',['GENE1','GENE4']),('is_missing',None),('is_not_missing',None)]: add(filters=[{'column':'Gene' if value else 'Note','operator':operator,'value':value}])
    for operator,value in [('lt',0.02),('gt',0.05),('gte',0.1),('lte',0.1),('between',[0,0.01])]: add(filters=[{'column':'GnomAD_AF','operator':operator,'value':value}])
    fingerprint=hashlib.sha256((target/'snappy.parquet').read_bytes()).hexdigest()
    identity=server.row_id('a'*64,fingerprint,0)
    for payload in [{'reviewed':True,'acmgOverride':'Pathogenic'}, {'acmgEvidence':[],'acmgClassification':'','acmgScore':0}, {'activeAcmgVersion':'svcv4','acmgOverride':'Pathogenic','svcv4Assessment':{'confirmed':True,'result':{'classification':'VUS','vusSubclass':'VUS-high','score':4,'state':'classified'}}}]:
        overlays=[{'rowId':identity,'version':3,'payload':payload}]
        add(overlays=overlays); add(overlays=overlays,export=True); add(overlays=overlays,filters=[{'column':'acmgClassification','operator':'equals','value':'VUS'}])
    add(rowId=identity); add(offset=100); add(limit=10000); add(search='GENE'); add(search='likely_benign')
    add(overlays=[{'rowId':identity,'version':1,'payload':{'svcv4Assessment':{'warnings':['中文<>&😀'], 'inputs':{'tiny':1e-6,'integerFloat':1.0}},'acmgEvidence':[{'code':'PP3','note':'中文😀','value':1.0}]}}],export=True)
    add(filters=[{'column':'unknown','operator':'equals','value':'x'}]); add(filters=[{'column':'Gene','operator':'unknown','value':'x'}]); add(filters=[{'column':'Position','operator':'between','value':[10,1]}])
    for op in ['equals','in','contains']:
        for value in [True,False,0,0.0,None]:
            add(overlays=[{'rowId':identity,'version':1,'payload':{'reviewed':True}}],filters=[{'column':'reviewed','operator':op,'value':[value] if op=='in' else value}])
    for direction in ['asc','desc']: add('nonfinite.parquet',sort='GnomAD_AF',direction=direction)
    for op,value in [('gt',0),('lt',0),('gte','NaN'),('between',['-inf','inf']),('between',['NaN','NaN'])]:
        add('nonfinite.parquet',filters=[{'column':'GnomAD_AF','operator':op,'value':value}])
    (target/'golden.json').write_text(json.dumps(cases,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    manifest={'cases':len(cases),'duckdb':duckdb.__version__,'sha256':{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(target.glob('*.parquet'))}}
    (target/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n',encoding='utf-8')
    if server.ASSESSMENT_ROOT.exists():
        for p in server.ASSESSMENT_ROOT.iterdir(): p.unlink()
        server.ASSESSMENT_ROOT.rmdir()
    print('PARQUET ORACLE',len(cases),'vectors')


if __name__=='__main__': main()
