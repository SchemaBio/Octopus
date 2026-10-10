import copy
import math
import pytest
import svcv4
import server


def assessment(**inputs):
    return {'disease':'OMIM:123456', 'moi':'AD', 'confirmed':False, 'inputs':inputs}


def test_no_data_is_not_zero_and_submitted_totals_are_ignored():
    data=assessment()
    data.update(result={'score':1000,'classification':'Pathogenic'},authoritative=True)
    result=svcv4.evaluate(data)
    assert result['authoritative'] is False
    assert result['result']['score'] is None
    assert result['result']['classification'] is None
    zero=svcv4.evaluate(assessment(population={'faf':0,'daft':0.001}))
    assert zero['result']['score']==0
    assert zero['result']['vusSubclass']=='VUS-low'


def test_population_reference_boundaries():
    for fold,points in [(0,0),(1.499,0),(1.5,-1),(4.999,-1),(5,-3),(14.999,-3),(15,-6)]:
        result=svcv4.evaluate(assessment(population={'faf':fold*0.0001,'daft':0.0001}))['result']
        assert result['score']==points
    assert result['classification']=='Benign'


def test_supported_workflows_empty_are_unclassified():
    for key in svcv4.WORKFLOWS:
        result=svcv4.evaluate(assessment(workflow=key,impact={}))['result']
        assert result['classification'] is None
    with pytest.raises(ValueError,match='暂不支持'):
        svcv4.evaluate(assessment(workflow='inframe_indel'))


def test_duplicate_related_probands_and_invalid_values_are_rejected():
    with pytest.raises(ValueError,match='文本'):
        svcv4.evaluate({**assessment(), 'disease':123})
    for cases in [[{'id':'p','family_id':'f'},{'id':'q','family_id':'f'}],[{}]]:
        with pytest.raises(ValueError,match='家系'):
            svcv4.evaluate(assessment(cases=cases))
    with pytest.raises(ValueError,match='有限'):
        svcv4.evaluate(assessment(population={'faf':math.inf}))
    with pytest.raises(ValueError,match='版本'):
        svcv4.evaluate({**assessment(), 'revision':'other'})


def test_schema_and_inputs_preserve_reference_revision():
    schema=svcv4.schema()
    assert schema['revision']==svcv4.REVISION
    assert schema['authoritative'] is False
    original=assessment(population={'faf':0,'daft':0.0001})
    before=copy.deepcopy(original)
    result=svcv4.evaluate(original)
    assert original==before
    assert result['result']['warnings']


def test_query_uses_selected_version_and_exports_provenance():
    expression=server.field_expr('acmgClassification', ['Type','Consequence','Transcript','AlphaMissense_AM'], 'snv-indel')
    assert "$.activeAcmgVersion" in expression
    assert "$.svcv4Assessment.result.classification" in expression
    assert '$.acmgOverride' in expression
    assert 'activeAcmgVersion' in server.OVERLAY_FIELDS
    assert 'acmgTrial' in server.OVERLAY_FIELDS
