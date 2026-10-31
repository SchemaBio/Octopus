# CNV gene TSV import contract

CNVAnno retains the original gene table as `Col4` through `Col19`, after
`Chromosome`, `Start`, and `End`. The workflow annotation adapter normalizes
types only for annotation and restores these original columns before publishing
the final table.

| Source | Meaning | Stored field |
| --- | --- | --- |
| Col4 | DEL / DUP / Normal | Type |
| Col8 | Exon count | ExonCount |
| Col9 | Weighted log2 mean | Log2Ratio |
| Col10 | Log2 median | Log2Median |
| Col11 | Log2 standard deviation | Log2Std |
| Col12 | Absolute copy number | CopyNumber |
| Col13 | Mean depth | Depth |
| Col14 | Mean bin weight | Weight |
| Col15 | Observed bin count | BinCount |
| Col16 | Matched segment count | SegmentCount |
| Col17 | Geometric coverage ratio | CoverageRatio |
| Col18 | HIGH / MEDIUM / LOW | ConfidenceLabel |
| Col19 | P-value | PValue |

Absolute copy number is never copied into `CopyRatio`. Unrelated legacy fields
such as `DepthRatio`, `Quality`, and `Impact` are not filled with positional
values from this layout. Unknown layouts and non-finite/out-of-range metrics
fail before any rows are persisted. Existing Parquet field mapping already uses
this layout and remains unchanged.

`model.CNVExon` adds nullable metric columns through the existing AutoMigrate
path. Deploy the updated Octopus backend with the workflow release. History
aggregation prefers absolute `copy_number`, derives a ratio by dividing it by
two, and retains the legacy ratio fallback for older records. These changes do
not rewrite previously imported rows; correcting historical imports requires
the original source files and a separate re-import.

The synthetic `testdata/cnv_gene_v1.tsv` fixture was generated from CNR values
through the workflow gene command, its annotation adapter, and CNVAnno commit
`6ac49dc6db96f8a0289e288525f929e2be0004b5`. It exercises the actual 52-column
boundary without a production database or patient data.

Run the focused backend regressions with:

```bash
go test ./internal/service ./internal/workflow ./internal/repository -run 'TestCNVGene|Test.*Progress|Test.*History' -count=1
```
