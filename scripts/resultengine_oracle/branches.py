"""Extract the pinned reference's explicit branch caps; not generated scores."""
import dataclasses
import importlib
import json
import sys
from pathlib import Path

root = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(root/'parquet-query/vendor/svcv4/src'))
branches = {}
for workflow in ['nonsense', 'frameshift', 'canonical_splice', 'intronic_synonymous',
                 'exon_deletion', 'exon_duplication', 'start_lost', 'stop_lost', 'missense_splice']:
    module = importlib.import_module('svcv4_model.scoring.pfd.'+workflow)
    branches[workflow] = {key.value: dataclasses.asdict(value) for key, value in module._BRANCH.items()}
(root/'internal/svcv4/branches.json').write_text(json.dumps(branches,indent=2)+'\n',encoding='utf-8')
