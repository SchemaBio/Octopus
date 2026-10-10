"""Development-only oracle. Never imported or shipped by the Go runtime."""
import dataclasses
import enum
import functools
import hashlib
import importlib
import inspect
import json
import math
import pkgutil
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path[:0] = [str(ROOT / 'parquet-query'), str(ROOT / 'parquet-query/vendor/svcv4/src'), str(ROOT / 'parquet-query/vendor/svcv4')]
import pytest
import svcv4
import svcv4_model.scoring as scoring


def normalize(value):
    if isinstance(value, enum.Enum): return value.value
    if hasattr(value, 'model_dump'): return value.model_dump(mode='json')
    if dataclasses.is_dataclass(value): return normalize(dataclasses.asdict(value))
    if hasattr(value, '_asdict'): return normalize(value._asdict())
    if isinstance(value, dict): return {str(k): normalize(v) for k, v in value.items()}
    if isinstance(value, (list, tuple, set, frozenset)): return [normalize(v) for v in value]
    if isinstance(value, float) and not math.isfinite(value): return repr(value)
    return value


class Capture:
    def __init__(self): self.cases = {}; self.test = ''
    def pytest_runtest_setup(self, item): self.test = item.nodeid.split('vendor/svcv4/')[-1]
    def pytest_configure(self, config):
        modules = [scoring] + [importlib.import_module(m.name) for m in pkgutil.walk_packages(scoring.__path__, scoring.__name__ + '.')]
        wrappers = {}
        for module in modules:
            for name, function in list(vars(module).items()):
                if not inspect.isfunction(function) or not function.__module__.startswith(scoring.__name__): continue
                if not (name.startswith('reference_') or function.__module__.endswith('.primitives')): continue
                if function not in wrappers:
                    signature = inspect.signature(function)
                    @functools.wraps(function)
                    def wrapped(*args, _fn=function, _sig=signature, **kwargs):
                        bound = _sig.bind(*args, **kwargs); bound.apply_defaults()
                        inputs = normalize(bound.arguments)
                        entry = {'function': _fn.__name__, 'module': _fn.__module__, 'input': inputs}
                        try:
                            output = _fn(*args, **kwargs)
                            entry['output'] = normalize(output)
                        except Exception as error:
                            entry['error'] = {'type': type(error).__name__, 'message': str(error)}
                            raise
                        finally:
                            encoded = json.dumps(entry, ensure_ascii=False, sort_keys=True, allow_nan=False)
                            self.cases[encoded] = entry
                        return output
                    wrappers[function] = wrapped
                setattr(module, name, wrappers[function])


def write(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False) + '\n', encoding='utf-8')


def main():
    capture = Capture()
    status = pytest.main(['-q', str(ROOT / 'parquet-query/vendor/svcv4/tests')], plugins=[capture])
    if status: raise SystemExit(status)
    cases = list(capture.cases.values())
    target = ROOT / 'internal/svcv4/testdata'
    write(target / 'upstream.json', cases)
    write(ROOT / 'internal/svcv4/schema.json', svcv4.schema())
    requests = [{'disease':'OMIM:123456', 'moi':'AD', 'inputs':{}, 'confirmed':False}]
    for case in cases:
        name = case['function']; data = case['input']
        if case.get('error'): continue
        request = {'disease':'OMIM:123456', 'moi':'AD', 'inputs':{}, 'confirmed':False}
        workflow = name.removeprefix('reference_score_')
        if workflow in svcv4.WORKFLOWS:
            request['inputs']={'workflow':workflow,'impact':data['assessment']}
            request['geneDiseaseValidity']=data.get('gene_disease_validity')
        elif name == 'reference_score_population':
            request['moi']=data.get('moi') or 'AD'; request['inputs']={'population':data['evidence']}
        elif name in ('reference_score_cln_proband','reference_score_loc_phe','reference_score_loc_seg'):
            request['moi']=data.get('moi') or 'AD'
            patient = data.get('case') or data.get('proband')
            if not patient: continue
            if name=='reference_score_cln_proband':
                patient={**patient,'id':'proband','family_id':'family'}
                request['inputs']={'cases':[patient],'population':{'faf':0,'daft':0.001}}
            else: request['inputs']={'family':patient}
        elif name == 'reference_score_cln_ccs': request['inputs']={'caseControl':data['evidence']}
        else: continue
        requests.append(request)
    requests += [
        {'disease':'Disease','moi':'AD','confirmed':True,'inputs':{'population':{'faf':0,'daft':0.001}}},
        {'disease':'Disease','moi':'AD','inputs':{'workflow':'inframe_indel'}},
        {'disease':'Disease','moi':'AD','revision':'wrong','inputs':{}},
        {'disease':None,'moi':'AD','inputs':{}},
        {'disease':'Disease','moi':'invalid','inputs':{}},
        {'disease':'Disease','moi':'AD','inputs':{'population':{'faf':-1}}},
        {'disease':'Disease','moi':'AD','inputs':{'cases':[{'id':'a','family_id':'same'},{'id':'b','family_id':'same'}]}},
    ]
    entries=[]
    for request in requests:
        try: entries.append({'input':request, 'output':svcv4.evaluate(request)})
        except Exception as error: entries.append({'input':request,'error':{'type':type(error).__name__,'message':str(error)}})
    write(target/'evaluate.json', entries)
    files=[target/'upstream.json',target/'evaluate.json',ROOT/'internal/svcv4/schema.json']
    write(target/'manifest.json',{'revision':svcv4.REVISION,'python':sys.version.split()[0], 'upstreamVectors':len(cases),'evaluateVectors':len(entries),'sha256':{str(p.relative_to(ROOT)).replace('\\','/'):hashlib.sha256(p.read_bytes()).hexdigest() for p in files}})
    print('ORACLE',len(cases),'upstream vectors;',len(entries),'adapter vectors')


if __name__ == '__main__': main()
