"""Pinned SVCv4 reference adapter. No network access or curator-supplied totals."""
import dataclasses
import importlib
import json
import math
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent / 'vendor/svcv4/src'))
from svcv4_model.case import Case, MOI, GeneDiseaseValidity
from svcv4_model.population import PopulationEvidence
from svcv4_model.case_control import CaseControlStudyEvidence
from svcv4_model import scoring

REVISION = 'ef66faff51a265fef7b5c4e6439905f3aa540c46'
SOURCE = 'https://github.com/clingen-data-model/svcv4-model/tree/' + REVISION
WORKFLOWS = {
    'missense': ('missense', 'MissenseAssessment', '错义'),
    'nonsense': ('nonsense', 'NonsenseAssessment', '无义'),
    'frameshift': ('frameshift', 'FrameshiftAssessment', '移码'),
    'canonical_splice': ('canonical_splice', 'CanonicalSpliceAssessment', '经典剪接'),
    'intronic_synonymous': ('intronic_synonymous', 'IntronicSynonymousAssessment', '内含子／同义'),
    'exon_deletion': ('exon_deletion', 'ExonDeletionAssessment', '外显子缺失'),
    'exon_duplication': ('exon_duplication', 'ExonDuplicationAssessment', '外显子重复'),
    'start_lost': ('start_lost', 'StartLostAssessment', '起始密码子丢失'),
    'stop_lost': ('stop_lost', 'StopLostAssessment', '终止密码子丢失'),
}
MODELS = {key: getattr(importlib.import_module('svcv4_model.' + mod), cls)
          for key, (mod, cls, _) in WORKFLOWS.items()}
CLASSIFICATIONS = {'PATHOGENIC': 'Pathogenic', 'LIKELY_PATHOGENIC': 'Likely_Pathogenic',
                   'VUS': 'VUS', 'LIKELY_BENIGN': 'Likely_Benign', 'BENIGN': 'Benign'}


def schema():
    return {'revision': REVISION, 'source': SOURCE, 'authoritative': False,
            'workflows': [{'id': key, 'label': value[2], 'schema': MODELS[key].model_json_schema()}
                          for key, value in WORKFLOWS.items()],
            'population': PopulationEvidence.model_json_schema(),
            'case': Case.model_json_schema(), 'caseControl': CaseControlStudyEvidence.model_json_schema(),
            'moi': [v.value for v in MOI], 'geneDiseaseValidity': [v.value for v in GeneDiseaseValidity],
            'unsupported': ['inframe_indel', 'non_coding']}


def finite(value):
    if isinstance(value, float) and not math.isfinite(value):
        raise ValueError('证据数值必须为有限数')
    if isinstance(value, dict):
        for v in value.values(): finite(v)
    if isinstance(value, list):
        for v in value: finite(v)


def evaluate(request):
    # Ignore all submitted derived fields: always recompute from the raw input.
    finite(request)
    if request.get('revision') not in (None, REVISION):
        raise ValueError('草案规则版本不匹配，请保留旧记录并新建评定')
    inputs = request.get('inputs', {})
    if not isinstance(inputs, dict) or len(json.dumps(inputs)) > 200000:
        raise ValueError('新版证据输入无效或过大')
    allowed = {'workflow', 'impact', 'population', 'cases', 'caseControl', 'family'}
    if set(inputs) - allowed: raise ValueError('未知新版证据字段')
    disease = request.get('disease', '')
    if not isinstance(disease, str): raise ValueError('疾病必须为文本')
    disease = disease.strip()
    moi = MOI(request['moi']) if request.get('moi') else None
    gdv = GeneDiseaseValidity(request['geneDiseaseValidity']) if request.get('geneDiseaseValidity') else None
    if not disease or len(disease) > 500 or moi is None:
        raise ValueError('请确认疾病及遗传模式')
    warnings = ['草案／非权威参考实现；ClinGen CSpec 为权威计分来源。',
                '上游尚未完整强制执行病例适用性及基因疾病有效性规则，请人工核验。']
    families = []
    details = {}
    workflow = inputs.get('workflow')
    if workflow:
        if workflow not in MODELS: raise ValueError('此工作流暂不支持')
        assessment = MODELS[workflow].model_validate(inputs.get('impact') or {})
        result = getattr(scoring, 'reference_score_' + workflow)(assessment, gene_disease_validity=gdv)
        families.append(result)
        details['impact'] = dataclasses.asdict(result)
    population = PopulationEvidence.model_validate(inputs.get('population') or {})
    if population.faf is not None and not 0 <= population.faf <= 1:
        raise ValueError('FAF 必须在0到1之间')
    if population.daft is not None and not 0 < population.daft <= 1:
        raise ValueError('DAFT 必须大于0且不超过1')
    if any(n is not None and n < 0 for n in (population.homozygote_count,population.hemizygote_count)):
        raise ValueError('出现次数不能为负数')
    pop = scoring.reference_score_population(population, moi=moi)
    families.append(scoring.reference_aggregate_pop([pop]))
    details['population'] = dataclasses.asdict(pop)
    cases = inputs.get('cases') or []
    if not isinstance(cases, list) or len(cases) > 100: raise ValueError('最多录入100个独立病例')
    parsed = [Case.model_validate(c) for c in cases]
    ids, family_ids = set(), set()
    for c in parsed:
        if not c.id or not c.family_id or c.id in ids or c.family_id in family_ids:
            raise ValueError('临床病例须填写唯一病例和家系编号，每个无关家系仅一个先证者')
        ids.add(c.id); family_ids.add(c.family_id)
    clinical = [scoring.reference_score_cln_proband(c, moi=moi) for c in parsed]
    ccs = scoring.reference_score_cln_ccs(CaseControlStudyEvidence.model_validate(inputs['caseControl'])) if inputs.get('caseControl') else None
    cln = scoring.reference_finalize_cln(scoring.reference_aggregate_cln_cases(clinical), ccs,
                                      pop_frq_points=pop.sub_code_points.get('POP_FRQ'))
    families.append(cln)
    details['cases'] = [dataclasses.asdict(r) for r in clinical]
    details['clinical'] = dataclasses.asdict(cln)
    if ccs: details['caseControl'] = dataclasses.asdict(ccs)
    if inputs.get('family'):
        c = Case.model_validate(inputs['family'])
        phe = scoring.reference_score_loc_phe(c, moi=moi)
        seg = scoring.reference_score_loc_seg(c, moi=moi)
        loc = scoring.reference_aggregate_loc([phe, seg])
        families.append(loc)
        details['family'] = {'phenotype': dataclasses.asdict(phe), 'segregation': dataclasses.asdict(seg), 'subtotal': dataclasses.asdict(loc)}
    combined = scoring.reference_combine_case(families)
    band = scoring.reference_classify(combined.parent_total) if combined.parent_total is not None else None
    # Preserve every upstream caveat and assumption, including the missense sub-paths.
    def provenance(value):
        if isinstance(value, dict):
            for key, v in value.items():
                if key == 'provenance': warnings.extend(v)
                else: provenance(v)
        elif isinstance(value, list):
            for v in value: provenance(v)
    provenance(details)
    warnings.extend(combined.provenance)
    return {'disease': disease, 'moi': moi.value, 'geneDiseaseValidity': gdv.value if gdv else None,
            'inputs': inputs, 'revision': REVISION, 'source': SOURCE, 'authoritative': False,
            'confirmed': request.get('confirmed') is True,
            'result': {'score': combined.parent_total, 'classification': CLASSIFICATIONS[band.category.value] if band else None,
                       'vusSubclass': band.vus_subclass.value if band and band.vus_subclass else None,
                       'state': 'classified' if band else 'insufficient_evidence',
                       'breakdown': combined.sub_code_points, 'details': details,
                       'warnings': list(dict.fromkeys(warnings))}}
