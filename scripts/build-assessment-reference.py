#!/usr/bin/env python3
"""Build immutable browser evidence resources from administrator-pinned sources.

No clinical data, credentials, or network requests are accepted. Source paths,
versions, SHA256 and licenses must be listed in a reviewed release manifest.
HPO graph semantics are preserved; information content is derived from positive
human disease annotations. Clinical rule supplements are explicit curated data.
"""
import argparse, csv, hashlib, io, json, math, os, re
from pathlib import Path

def load_source(root, entry):
    path=(root/entry['file']).resolve()
    if not path.is_relative_to(root.resolve()): raise ValueError('source outside release directory')
    raw=path.read_bytes()
    if len(raw)>256*1024*1024 or hashlib.sha256(raw).hexdigest()!=entry['sha256']:raise ValueError('source checksum/size mismatch')
    if not all(entry.get(k) for k in ('version','source','license','accessedAt')):raise ValueError('source attribution is incomplete')
    return raw.decode('utf-8-sig')

def ontology(text):
    terms={}
    for block in text.split('[Term]')[1:]:
        ident=re.search(r'^id: (HP:\d{7})$',block,re.M)
        if not ident or re.search(r'^is_obsolete: true$',block,re.M):continue
        terms[ident[1]]={'id':ident[1],'parents':re.findall(r'^is_a: (HP:\d{7})',block,re.M),'ancestors':[],'ic':0}
    def ancestors(ident,stack):
        if ident in stack:raise ValueError('HPO cycle')
        term=terms[ident]
        if term.get('_done'):return set(term['ancestors'])
        result=set()
        for parent in term['parents']:
            if parent not in terms:continue
            result.add(parent);result.update(ancestors(parent,stack|{ident}))
        term['ancestors']=sorted(result);term['_done']=True;return result
    for ident in terms:ancestors(ident,set())
    return terms

def phenotype_annotations(text,terms):
    lines=[x for x in text.splitlines() if not x.startswith('#') and x.strip()]
    reader=csv.DictReader(lines,delimiter='\t')
    diseases={}
    for row in reader:
        ident=row.get('database_id') or row.get('DatabaseID')
        term=row.get('hpo_id') or row.get('HPO_ID')
        if not ident or term not in terms or (row.get('aspect') and row['aspect']!='P') or 'NOT' in (row.get('qualifier') or row.get('Qualifier') or '').split('|'):continue
        diseases.setdefault(ident,set()).add(term)
    by_term={key:set() for key in terms}
    for disease,values in diseases.items():
        for term in values:
            for parent in [term,*terms[term]['ancestors']]:by_term[parent].add(disease)
    total=len(diseases)
    for key,value in terms.items():
        value['ic']=-math.log2(len(by_term[key])/total) if total and by_term[key] else 0
        value.pop('parents',None);value.pop('_done',None)
    return diseases

def dosage(text,reference,source):
    header=None;rows=[]
    for line in text.splitlines():
        if line.startswith('#Gene Symbol') or line.startswith('#ISCA ID'):
            header=line.lstrip('#').split('\t');continue
        if line.startswith('#') or not line.strip():continue
        if not header:raise ValueError('unknown ClinGen TSV header')
        row=dict(zip(header,line.split('\t')))
        match=re.fullmatch(r'(?:chr)?([\w]+):(\d+)-(\d+)',row.get('Genomic Location',''))
        if not match:continue
        # ClinGen TSV genomic locations are one-based inclusive. Do not apply
        # this conversion to BED resources, which are already half-open.
        start,end=int(match[2])-1,int(match[3])
        if start<0 or end<=start:raise ValueError('invalid dosage interval')
        score=lambda key:int(row[key]) if row.get(key,'').isdigit() else -1
        rows.append({'reference':reference,'chromosome':match[1].replace('M','MT') if match[1]=='M' else match[1],'start':start,'end':end,'id':row.get('ISCA ID') or row.get('Gene Symbol'),'gene':row.get('Gene Symbol'),'hi':score('Haploinsufficiency Score'),'ts':score('Triplosensitivity Score'),'source':source})
    return rows

def build(manifest,root):
    reference=manifest['reference']
    if reference not in ('hg19','hg38'):raise ValueError('reference must be explicit')
    sources={key:load_source(root,value) for key,value in manifest['sources'].items()}
    terms=ontology(sources['hpo']) if 'hpo' in sources else {}
    annotations=phenotype_annotations(sources['hpoa'],terms) if 'hpoa' in sources else {}
    pack={'version':manifest['version'],'reference':reference,'licenses':[{k:entry[k] for k in ('source','license','accessedAt')} for entry in manifest['sources'].values()],'hpo':terms,'diseases':[],'dosage':[],'str':[],'imprinting':[]}
    for key in ('dosage_genes','dosage_regions'):
        if key in sources:pack['dosage'].extend(dosage(sources[key],reference,manifest['sources'][key]['source']))
    if 'validity' in sources and 'mondo' in sources:
        equivalents={}
        for block in sources['mondo'].split('[Term]')[1:]:
            ident=re.search(r'^id: (MONDO:\d+)$',block,re.M)
            if not ident or re.search(r'^is_obsolete: true$',block,re.M):continue
            # General xrefs are not necessarily equivalent diseases. Only use
            # Mondo's explicitly declared equivalence, never a parent grouping.
            ids=[]
            for line in block.splitlines():
                if 'MONDO:equivalentTo' not in line:continue
                match=re.match(r'xref: ((?:OMIM|Orphanet):\d+)',line)
                if match:ids.append(match[1].replace('Orphanet:','ORPHA:'))
            equivalents[ident[1]]=ids
        records=list(csv.reader(io.StringIO(sources['validity'])))
        header=next((i for i,r in enumerate(records) if r and r[0]=='GENE SYMBOL'),None)
        if header is None:raise ValueError('unknown ClinGen validity schema')
        for values in records[header+1:]:
            row=dict(zip(records[header],values))
            if row.get('CLASSIFICATION') not in ('Moderate','Strong','Definitive') or row.get('MOI') not in ('AD','AR','XL','MT'):continue
            ident=row['DISEASE ID (MONDO)'];phenotypes=set()
            for equivalent in [ident,*equivalents.get(ident,[])]:phenotypes.update(annotations.get(equivalent,[]))
            if not phenotypes:continue
            pack['diseases'].append({'id':ident,'gene':row['GENE SYMBOL'],'hpo':sorted(phenotypes),'validity':row['CLASSIFICATION'],'inheritance':row['MOI'],'source':row['ONLINE REPORT']})
    # The clinical supplement includes reviewed disease associations/thresholds,
    # not raw gene-name matches or unverified disease titles from result reports.
    if 'clinical' in sources:
        clinical=json.loads(sources['clinical'])
        for d in clinical.get('diseases',[]):
            if d.get('validity') not in ('Moderate','Strong','Definitive') or d.get('inheritance') not in ('AD','AR','XL','MT'):raise ValueError('unverified disease association')
            if 'hpoAnnotationId' in d:d['hpo']=sorted(annotations.get(d.pop('hpoAnnotationId'),[]))
            if any(t not in terms for t in d.get('hpo',[])):raise ValueError('unknown HPO term')
            pack['diseases'].append(d)
        for key in ('str','imprinting'):
            for record in clinical.get(key,[]):
                if record.get('reference')!=reference or not isinstance(record.get('start'),int) or record['start']<0 or record.get('end',0)<=record['start'] or not record.get('source'):raise ValueError('unverified locus')
                pack[key].append(record)
    pack['sourceManifestSha256']=hashlib.sha256(json.dumps(manifest,sort_keys=True).encode()).hexdigest()
    return pack

def main():
    parser=argparse.ArgumentParser();parser.add_argument('--manifest',type=Path,required=True);parser.add_argument('--output',type=Path,required=True);args=parser.parse_args()
    manifest=json.loads(args.manifest.read_text(encoding='utf-8'));pack=build(manifest,args.manifest.parent)
    encoded=json.dumps(pack,separators=(',',':'),ensure_ascii=False).encode()
    if len(encoded)>32*1024*1024:raise ValueError('reference package exceeds browser budget; shard the release')
    args.output.mkdir(parents=True,exist_ok=True)
    content_hash=hashlib.sha256(encoded).hexdigest()
    immutable=args.output/(content_hash+'.json')
    if not immutable.exists():immutable.write_bytes(encoded)
    target=args.output/(manifest['reference']+'.json');temp=target.with_suffix('.json.part');temp.write_bytes(encoded);os.replace(temp,target)
    print(json.dumps({'version':pack['version'],'sha256':content_hash,'bytes':len(encoded),'hpo':len(pack['hpo']),'diseases':len(pack['diseases']),'dosage':len(pack['dosage'])}))
if __name__=='__main__':main()
