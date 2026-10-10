import argparse,duckdb,pathlib,json
parser=argparse.ArgumentParser(description='Generate entirely synthetic cells from aggregate Parquet shape statistics; never copy clinical or genetic values')
parser.add_argument('--source',type=pathlib.Path,required=True)
parser.add_argument('--output',type=pathlib.Path,required=True)
args=parser.parse_args()
source=max(args.source.rglob('*.parquet'),key=lambda p:p.stat().st_size) if args.source.is_dir() else args.source
args.output.parent.mkdir(parents=True,exist_ok=True)
con=duckdb.connect()
schema=con.execute('DESCRIBE SELECT * FROM read_parquet(?)',[str(source)]).fetchall()
count=con.execute('SELECT count(*) FROM read_parquet(?)',[str(source)]).fetchone()[0]
# Only aggregate shape statistics are read. Every generated cell is synthetic,
# including coordinates, chromosome, frequencies, genes and numeric fields.
expr=[];stats=[]
for name,typ,*_ in schema:
 q='"'+name.replace('"','""')+'"'
 if typ=='VARCHAR':
  avg,nulls=con.execute('SELECT avg(length('+q+')),count(*) FILTER(WHERE '+q+' IS NULL) FROM read_parquet(?)',[str(source)]).fetchone()
  length=min(4096,max(1,round(avg or 1)))
  synthetic={'Chromosome':"'chr'||CAST(i%22+1 AS VARCHAR)",'Position':'CAST(i*13 AS VARCHAR)','Type':"'SNP'",'Consequence':"'missense_variant'",'Transcript':"'ENST000001'",'Gene':"'GENE'||CAST(i%5000 AS VARCHAR)",'GnomAD_AF':'CAST((i%100)/100000.0 AS VARCHAR)','AlphaMissense_AM':'CAST((i%1000)/1000.0 AS VARCHAR)'}.get(name,"repeat('x',"+str(length)+")")
  expression='CASE WHEN i%1000<'+str(round(1000*nulls/count) if count else 0)+' THEN NULL ELSE '+synthetic+' END'
  stats.append({'type':typ,'meanLength':length,'nullPerThousand':round(1000*nulls/count) if count else 0})
 elif typ=='BOOLEAN': expression='i%2=0';stats.append({'type':typ})
 elif typ.startswith(('INTEGER','BIGINT','SMALLINT','TINYINT','UINTEGER','UBIGINT','DOUBLE','FLOAT','DECIMAL')):expression='CAST(i%100 AS '+typ+')';stats.append({'type':typ})
 else:expression='CAST(NULL AS '+typ+')';stats.append({'type':typ})
 expr.append(expression+' AS '+q)
dest=args.output
con.execute("COPY (SELECT "+','.join(expr)+" FROM range("+str(count)+") t(i)) TO '"+str(dest)+"' (FORMAT PARQUET,COMPRESSION SNAPPY,ROW_GROUP_SIZE 65536)")
print(json.dumps({'rows':count,'columns':len(schema),'bytes':dest.stat().st_size,'basis':'aggregate schema/length/null statistics only; every cell synthetic','statistics':stats}))
