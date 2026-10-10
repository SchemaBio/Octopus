"""Freeze Pydantic coercion/default/error behavior at the public JSON boundary."""
import copy
import json
import sys
from pathlib import Path

root=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(root/'parquet-query'))
import svcv4

requests=[]
base={'disease':'Disease','moi':'AD','geneDiseaseValidity':'MODERATE','confirmed':True,'inputs':{}}
def add(inputs): requests.append({**base,'inputs':inputs})
for workflow in svcv4.WORKFLOWS:
    for payload in [None,{},[],False,0,'bad',{'unexpected':0}]:
        add({'workflow':workflow,'impact':payload})
    for value in [None,0,False,'0','2.5','NaN','inf','-inf','bad',[],{}]:
        key='amino_acid' if workflow=='missense' else None
        impact={'predictive':{'initial_points':value},'fxn_points':0}
        if key: impact={key:impact}
        add({'workflow':workflow,'impact':impact})
for name,fields in [('population',['faf','daft','homozygote_count','hemizygote_count','hmz_eligible']),
                    ('caseControl',['odds_ratio','case_variant_count','case_cohort_size','controls_matched','ascertainment_bias_considered']),
                    ('family',['testing','relatives','vbc_zygosity','additional_variants','pheno_specificity_for_mde'])]:
    for field in fields:
        for value in [None,0,1,False,True,'0','1','true','FALSE','UNKNOWN','invalid',[],{}]:
            add({name:{field:value}})
for value in [None,[],{},False,True,0,1,'bad']:
    add({'cases':value}); add({'workflow':value})
for count in [1000,40000]: add({'family':{'id':'汉'*count}})
entries=[]
for request in requests:
    try:
        output=svcv4.evaluate(copy.deepcopy(request))
        # The old Go HTTP client rejects NaN/Infinity tokens in a response.
        json.dumps(output,allow_nan=False)
        entries.append({'input':request,'output':output})
    except Exception as error:
        entries.append({'input':request,'error':{'type':type(error).__name__,'message':str(error)}})
(root/'internal/svcv4/testdata/validation.json').write_text(json.dumps(entries,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
print('VALIDATION ORACLE',len(entries),'vectors')
