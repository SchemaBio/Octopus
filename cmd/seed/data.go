package main

import (
	"fmt"
	"time"

	"github.com/SchemaBio/Octopus/internal/model"
)

func ptrInt(v int) *int { return &v }

func ptrF64(v float64) *float64 { return &v }

func buildSamples(adminID uint, now time.Time) []model.Sample {
	age8 := 8
	age35 := 35
	age33 := 33
	age12 := 12

	mk := func(uuid, internalID string, gender model.SampleGender, age *int, batch, diagnosis, remark string) model.Sample {
		s := model.Sample{
			UUID:              uuid,
			InternalID:        internalID,
			Gender:            gender,
			Age:               age,
			SampleType:        model.SampleTypeWholeBlood,
			Batch:             batch,
			ClinicalDiagnosis: diagnosis,
			Remark:            remark + " " + seedMarker,
			Status:            model.SampleStatusCompleted,
			CreatedBy:         adminID,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		s.SetHPOTerms([]model.HPOTerm{
			{ID: "HP:0001250", Name: "Seizure"},
			{ID: "HP:0001263", Name: "Global developmental delay"},
		})
		s.SetMatchedPair(&model.MatchedPair{
			R1Path: fmt.Sprintf("seed/%s_R1.fastq.gz", internalID),
			R2Path: fmt.Sprintf("seed/%s_R2.fastq.gz", internalID),
		})
		s.SetSubmissionInfo(model.SubmissionInfo{
			SubmissionDate:       now.AddDate(0, 0, -14).Format("2006-01-02"),
			SampleCollectionDate: now.AddDate(0, 0, -20).Format("2006-01-02"),
			SampleReceiveDate:    now.AddDate(0, 0, -18).Format("2006-01-02"),
			SampleQuality:        "good",
		})
		s.SetProjectInfo(model.ProjectInfo{
			ProjectID:      "seed-project-1",
			ProjectName:    "Seed WES Demo",
			TestItems:      []string{"WES", "CNV"},
			Panel:          "ClinicalExome",
			TurnaroundDays: 14,
			Priority:       "normal",
		})
		s.SetFamilyHistory(model.FamilyHistoryInfo{
			HasHistory: true,
			AffectedMembers: []model.AffectedMember{
				{Relation: "sibling", Condition: "developmental delay", OnsetAge: "2"},
			},
			PedigreeNote: "Seed trio pedigree",
		})
		return s
	}

	return []model.Sample{
		mk(sampleProbandUUID, "SEED-PROBAND-001", model.SampleGenderMale, &age8, "SEED-BATCH-01", "Developmental delay; epilepsy", "Proband"),
		mk(sampleFatherUUID, "SEED-FATHER-001", model.SampleGenderMale, &age35, "SEED-BATCH-01", "Unaffected father", "Father"),
		mk(sampleMotherUUID, "SEED-MOTHER-001", model.SampleGenderFemale, &age33, "SEED-BATCH-01", "Unaffected mother", "Mother"),
		mk(sampleOtherUUID, "SEED-SOLO-002", model.SampleGenderFemale, &age12, "SEED-BATCH-02", "Hearing loss", "Solo sample"),
	}
}

func buildPipelines(adminID uint, now time.Time) []model.Pipeline {
	return []model.Pipeline{
		{
			ID:              pipelineWESUUID,
			Name:            "Seed Single WES",
			BaseType:        model.PipelineBaseWESSingle,
			Version:         "1.0.0",
			Description:     "Demo WES pipeline for UI development " + seedMarker,
			BEDFile:         "seed/clinical_exome.bed",
			ReferenceGenome: "hg19",
			CNVBaseline:     "seed/cnv_baseline",
			Status:          model.PipelineStatusActive,
			CreatedBy:       adminID,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		{
			ID:              pipelinePanelUUID,
			Name:            "Seed Family WES",
			BaseType:        model.PipelineBaseWESFamily,
			Version:         "0.9.0",
			Description:     "Demo family WES pipeline " + seedMarker,
			BEDFile:         "seed/neuro_panel.bed",
			ReferenceGenome: "hg38",
			CNVBaseline:     "",
			Status:          model.PipelineStatusActive,
			CreatedBy:       adminID,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
	}
}

func buildGeneLists(adminID uint, now time.Time) []model.GeneList {
	core := model.GeneList{
		ID:              geneListCoreUUID,
		Name:            "Seed Core Clinical",
		Description:     "Core genes used by seed SNV rows " + seedMarker,
		Category:        model.GeneListCategoryCore,
		DiseaseCategory: "Neurodevelopmental",
		CreatedBy:       adminID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	core.SetGenes([]string{
		"BRCA1", "BRCA2", "SCN1A", "MECP2", "TTN", "DMD", "CFTR", "PAH", "GJB2", "ATP7B",
	})

	imp := model.GeneList{
		ID:              geneListImpUUID,
		Name:            "Seed Important Extra",
		Description:     "Secondary gene list " + seedMarker,
		Category:        model.GeneListCategoryImportant,
		DiseaseCategory: "Metabolic",
		CreatedBy:       adminID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	imp.SetGenes([]string{"G6PD", "HBB", "HBA1", "SMN1", "FBN1"})

	return []model.GeneList{core, imp}
}

func buildPedigree(adminID uint, now time.Time) (model.Pedigree, []model.PedigreeMember) {
	p := model.Pedigree{
		ID:              pedigreeUUID,
		Name:            "SEED-PED-001",
		Disease:         "Developmental delay",
		Note:            "Three-generation demo pedigree " + seedMarker,
		ProbandMemberID: memberProbandUUID,
		CreatedBy:       adminID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	by := func(y int) *int { return &y }

	members := []model.PedigreeMember{
		{
			ID: memberPGFUUID, PedigreeID: pedigreeUUID, Name: "Paternal Grandfather",
			Gender: model.GenderMale, BirthYear: by(1950), Relation: model.RelationGrandfatherPaternal,
			AffectedStatus: model.AffectedStatusUnaffected, Generation: 1, Position: 1,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: memberPGMUUID, PedigreeID: pedigreeUUID, Name: "Paternal Grandmother",
			Gender: model.GenderFemale, BirthYear: by(1952), Relation: model.RelationGrandmotherPaternal,
			AffectedStatus: model.AffectedStatusUnaffected, Generation: 1, Position: 2,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: memberFatherUUID, PedigreeID: pedigreeUUID, SampleID: sampleFatherUUID, Name: "Father",
			Gender: model.GenderMale, BirthYear: by(1985), Relation: model.RelationFather,
			AffectedStatus: model.AffectedStatusUnaffected, FatherID: memberPGFUUID, MotherID: memberPGMUUID,
			Generation: 2, Position: 1, HasSample: true, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: memberMotherUUID, PedigreeID: pedigreeUUID, SampleID: sampleMotherUUID, Name: "Mother",
			Gender: model.GenderFemale, BirthYear: by(1987), Relation: model.RelationMother,
			AffectedStatus: model.AffectedStatusUnaffected, Generation: 2, Position: 2, HasSample: true,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: memberProbandUUID, PedigreeID: pedigreeUUID, SampleID: sampleProbandUUID, Name: "Proband",
			Gender: model.GenderMale, BirthYear: by(2016), Relation: model.RelationProband,
			AffectedStatus: model.AffectedStatusAffected, FatherID: memberFatherUUID, MotherID: memberMotherUUID,
			Generation: 3, Position: 1, HasSample: true,
			Phenotypes: map[string]interface{}{"values": []string{"seizure", "developmental delay"}},
			CreatedAt:  now, UpdatedAt: now,
		},
		{
			ID: memberSiblingUUID, PedigreeID: pedigreeUUID, Name: "Sibling",
			Gender: model.GenderFemale, BirthYear: by(2018), Relation: model.RelationSibling,
			AffectedStatus: model.AffectedStatusUnknown, FatherID: memberFatherUUID, MotherID: memberMotherUUID,
			Generation: 3, Position: 2, CreatedAt: now, UpdatedAt: now,
		},
	}
	return p, members
}

func buildTasks(adminID uint, now time.Time) []model.Task {
	finished := now.Add(-2 * time.Hour)
	startedRunning := now.Add(-30 * time.Minute)
	failedAt := now.Add(-1 * time.Hour)

	return []model.Task{
		{
			ID: taskCompletedUUID, UUID: taskCompletedUUID,
			Name: "Seed WES completed", SampleID: sampleProbandUUID, InternalID: "SEED-PROBAND-001",
			Pipeline: "Seed Single WES", PipelineVersion: "1.0.0", Template: "SingleWES",
			Executor: model.ExecutorLocal, Status: model.TaskStatusCompleted, Progress: 100,
			Remark: seedMarker + " completed with results", CreatedBy: adminID,
			StartedAt: &finished, FinishedAt: &finished,
			ResultImportStatus: model.ResultImportStatusSuccess, ResultImportedAt: &finished,
			CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now,
		},
		{
			ID: taskRunningUUID, UUID: taskRunningUUID,
			Name: "Seed WES running", SampleID: sampleOtherUUID, InternalID: "SEED-SOLO-002",
			Pipeline: "Seed Single WES", PipelineVersion: "1.0.0", Template: "SingleWES",
			Executor: model.ExecutorLocal, Status: model.TaskStatusRunning, Progress: 45,
			Remark: seedMarker + " running (no miniwdl)", CreatedBy: adminID,
			StartedAt: &startedRunning, ResultImportStatus: model.ResultImportStatusPending,
			CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now,
		},
		{
			ID: taskQueuedUUID, UUID: taskQueuedUUID,
			Name: "Seed WES queued", SampleID: sampleFatherUUID, InternalID: "SEED-FATHER-001",
			Pipeline: "Seed Neuro Panel", PipelineVersion: "0.9.0", Template: "SingleWES",
			Executor: model.ExecutorLocal, Status: model.TaskStatusQueued, Progress: 0,
			Remark: seedMarker + " queued", CreatedBy: adminID,
			ResultImportStatus: model.ResultImportStatusPending,
			CreatedAt:          now.Add(-20 * time.Minute), UpdatedAt: now,
		},
		{
			ID: taskFailedUUID, UUID: taskFailedUUID,
			Name: "Seed WES failed", SampleID: sampleMotherUUID, InternalID: "SEED-MOTHER-001",
			Pipeline: "Seed Single WES", PipelineVersion: "1.0.0", Template: "SingleWES",
			Executor: model.ExecutorLocal, Status: model.TaskStatusFailed, Progress: 12,
			Remark: seedMarker + " failed demo", Error: "seed: simulated pipeline failure",
			CreatedBy: adminID, FinishedAt: &failedAt,
			ResultImportStatus: model.ResultImportStatusFailed, ResultImportError: "seed failure",
			CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now,
		},
	}
}

func buildQC(now time.Time) model.QCResult {
	_ = now
	return model.QCResult{
		ID:                   qcResultID,
		TaskID:               taskCompletedUUID,
		SampleID:             "SEED-PROBAND-001",
		TotalReads:           120_000_000,
		TotalBases:           18_000_000_000,
		Q20Rate:              0.97,
		Q30Rate:              0.92,
		GcContent:            0.48,
		Read1MeanLength:      150,
		Read2MeanLength:      150,
		AverageDepth:         98.5,
		DedupDepth:           86.2,
		CoverageGt0x:         0.995,
		CoverageGte30x:       0.96,
		CoverageGte100x:      0.72,
		MappedReads:          118_500_000,
		MappedReadsFraction:  0.987,
		InsertSizeAverage:    320,
		InsertSizeMedian:     310,
		RegionLength:         45_000_000,
		TargetDataFraction:   0.72,
		MeanTargetCoverage:   95.0,
		MedianTargetCoverage: 90,
		PctTargetBases30x:    0.96,
		PctTargetBases100x:   0.70,
		FoldEnrichment:       42.5,
		ZeroCvgTargetsPct:    0.4,
		DuplicateRate:        0.12,
		PredictedGender:      "Male",
		SryCount:             1200,
		MtAverageDepth:       2100,
		MtCoverageGt0x:       0.999,
		FingerprintHash:      "seed-fp-001",
		PfMismatchRate:       0.004,
	}
}

type snvSpec struct {
	gene, chrom, ref, alt, vtype, zyg, consequence, transcript, hgvsc, hgvsp string
	pos                                                                      int64
	depth                                                                    int
	vaf                                                                      float64
	acmg                                                                     model.ACMGClassification
	reviewed                                                                 bool
}

func buildSNVs(now time.Time) []model.SNVIndel {
	specs := []snvSpec{
		{"SCN1A", "chr2", "C", "T", "SNV", "Heterozygous", "missense_variant", "NM_001165963.4", "c.664C>T", "p.Arg222Ter", 166848648, 120, 0.48, model.ACMGPathogenic, true},
		{"MECP2", "chrX", "G", "A", "SNV", "Hemizygous", "stop_gained", "NM_004992.4", "c.502C>T", "p.Arg168Ter", 153296777, 88, 0.95, model.ACMGPathogenic, true},
		{"BRCA1", "chr17", "G", "A", "SNV", "Heterozygous", "missense_variant", "NM_007294.4", "c.181T>G", "p.Cys61Gly", 43124028, 110, 0.51, model.ACMGLikelyPathogenic, false},
		{"BRCA2", "chr13", "A", "G", "SNV", "Heterozygous", "synonymous_variant", "NM_000059.4", "c.3396A>G", "p.Lys1132=", 32914438, 95, 0.49, model.ACMGBenign, false},
		{"TTN", "chr2", "C", "T", "SNV", "Heterozygous", "missense_variant", "NM_001267550.2", "c.12345C>T", "p.Arg4115Cys", 179410000, 80, 0.45, model.ACMGVUS, false},
		{"DMD", "chrX", "T", "C", "SNV", "Hemizygous", "intron_variant", "NM_004006.3", "c.93+5T>C", "", 32844682, 70, 0.90, model.ACMGVUS, false},
		{"CFTR", "chr7", "C", "T", "SNV", "Heterozygous", "missense_variant", "NM_000492.4", "c.1521_1523del", "p.Phe508del", 117199646, 100, 0.50, model.ACMGPathogenic, false},
		{"PAH", "chr12", "G", "A", "SNV", "Homozygous", "missense_variant", "NM_000277.3", "c.1222C>T", "p.Arg408Trp", 103234273, 130, 0.99, model.ACMGPathogenic, true},
		{"GJB2", "chr13", "G", "A", "SNV", "Heterozygous", "missense_variant", "NM_004004.6", "c.109G>A", "p.Val37Ile", 20763612, 105, 0.47, model.ACMGLikelyPathogenic, false},
		{"ATP7B", "chr13", "C", "T", "SNV", "Heterozygous", "missense_variant", "NM_000053.4", "c.2333G>T", "p.Arg778Leu", 52524471, 90, 0.46, model.ACMGLikelyPathogenic, false},
		{"HBB", "chr11", "A", "T", "SNV", "Heterozygous", "missense_variant", "NM_000518.5", "c.20A>T", "p.Glu7Val", 5248232, 140, 0.48, model.ACMGPathogenic, false},
		{"FBN1", "chr15", "C", "T", "SNV", "Heterozygous", "missense_variant", "NM_000138.5", "c.3509G>A", "p.Arg1170His", 48744758, 85, 0.44, model.ACMGVUS, false},
		{"SMN1", "chr5", "C", "T", "SNV", "Heterozygous", "splice_acceptor_variant", "NM_000344.4", "c.833-2A>G", "", 70247773, 75, 0.42, model.ACMGPathogenic, false},
		{"G6PD", "chrX", "C", "T", "SNV", "Hemizygous", "missense_variant", "NM_001042351.3", "c.202G>A", "p.Val68Met", 154535277, 92, 0.96, model.ACMGLikelyBenign, false},
		{"HBA1", "chr16", "G", "A", "SNV", "Heterozygous", "missense_variant", "NM_000558.5", "c.427T>C", "p.Ter143Gln", 226679, 100, 0.50, model.ACMGVUS, false},
	}

	// Expand to ~45 rows with synthetic neighbors for table pagination demos.
	out := make([]model.SNVIndel, 0, 50)
	for i, sp := range specs {
		out = append(out, makeSNV(i, sp, now))
	}
	extraGenes := []string{"SCN2A", "STXBP1", "CDKL5", "KCNQ2", "GRIN2A", "SYNGAP1", "SHANK3", "UBE3A", "TSC1", "TSC2",
		"NF1", "PTEN", "TP53", "MLH1", "MSH2", "MSH6", "PMS2", "APC", "RET", "VHL",
		"LDLR", "APOB", "PCSK9", "MYH7", "MYBPC3", "LMNA", "COL1A1", "COL3A1", "TGFBR1", "TGFBR2"}
	acmgs := []model.ACMGClassification{model.ACMGVUS, model.ACMGLikelyBenign, model.ACMGBenign, model.ACMGVUS, model.ACMGLikelyPathogenic}
	for i, g := range extraGenes {
		sp := snvSpec{
			gene: g, chrom: fmt.Sprintf("chr%d", (i%22)+1), ref: "C", alt: "T", vtype: "SNV",
			zyg: "Heterozygous", consequence: "missense_variant", transcript: "NM_SEED.1",
			hgvsc: fmt.Sprintf("c.%dC>T", 100+i*3), hgvsp: fmt.Sprintf("p.Arg%dCys", 30+i),
			pos: int64(1000000 + i*1000), depth: 60 + i, vaf: 0.40 + float64(i%10)*0.01,
			acmg: acmgs[i%len(acmgs)], reviewed: i%7 == 0,
		}
		out = append(out, makeSNV(100+i, sp, now))
	}
	return out
}

func makeSNV(idx int, sp snvSpec, now time.Time) model.SNVIndel {
	id := fmt.Sprintf("77777777-7777-4777-8777-%012d", idx+1)
	row := model.SNVIndel{
		ID:                 id,
		TaskID:             taskCompletedUUID,
		Chromosome:         sp.chrom,
		Position:           sp.pos,
		VariantID:          fmt.Sprintf("%s-%d-%s-%s", sp.chrom, sp.pos, sp.ref, sp.alt),
		Ref:                sp.ref,
		Alt:                sp.alt,
		VariantType:        sp.vtype,
		Quality:            99,
		Filter:             "PASS",
		Genotype:           "0/1",
		Zygosity:           sp.zyg,
		Depth:              sp.depth,
		VAF:                sp.vaf,
		Gene:               sp.gene,
		Transcript:         sp.transcript,
		Consequence:        sp.consequence,
		Impact:             "MODERATE",
		HGVSc:              sp.hgvsc,
		HGVSp:              sp.hgvsp,
		ACMGClassification: sp.acmg,
		DiseaseAssociation: "seed demo association",
		InheritanceMode:    "AD",
		RsID:               fmt.Sprintf("rsSEED%d", idx+1),
	}
	if sp.reviewed {
		row.Reviewed = true
		row.ReviewedBy = "admin@octopus.local"
		t := now.Add(-time.Hour)
		row.ReviewedAt = &t
	}
	return row
}

func buildCNVSegments(now time.Time) []model.CNVSegment {
	_ = now
	type seg struct {
		chrom, typ, genes, class string
		start, end               int64
	}
	specs := []seg{
		{"chr1", "DEL", "GJB3,GJB4", "Pathogenic", 1500000, 1650000},
		{"chr7", "DUP", "AUTS2", "Likely_Pathogenic", 69000000, 70200000},
		{"chr15", "DEL", "UBE3A,GABRB3", "Pathogenic", 25000000, 25500000},
		{"chr17", "DUP", "PMP22", "Pathogenic", 14000000, 15500000},
		{"chr22", "DEL", "TBX1", "Likely_Pathogenic", 18000000, 21500000},
		{"chr2", "DEL", "NRXN1", "VUS", 50000000, 50150000},
		{"chr16", "DUP", "SH2B1", "VUS", 28800000, 29050000},
		{"chrX", "DEL", "DMD", "Pathogenic", 31000000, 32000000},
		{"chr3", "Normal", "CNTN4", "Benign", 2000000, 2100000},
		{"chr5", "DUP", "NSD1", "Likely_Benign", 176500000, 176800000},
		{"chr11", "DEL", "WT1", "VUS", 32400000, 32480000},
		{"chr9", "DUP", "TSC1", "VUS", 135700000, 135900000},
	}
	out := make([]model.CNVSegment, 0, len(specs))
	for i, s := range specs {
		cn := 1.0
		if s.typ == "DUP" {
			cn = 3.0
		} else if s.typ == "Normal" {
			cn = 2.0
		}
		out = append(out, model.CNVSegment{
			ID:             fmt.Sprintf("88888888-8888-4888-8888-%012d", i+1),
			TaskID:         taskCompletedUUID,
			Chromosome:     s.chrom,
			StartPosition:  s.start,
			EndPosition:    s.end,
			Type:           s.typ,
			CopyRatio:      ptrF64(cn / 2),
			GeneCount:      1 + i%3,
			DosageGenes:    s.genes,
			Classification: s.class,
			Reason:         "seed demo CNV segment",
		})
	}
	return out
}

func buildCNVExons(now time.Time) []model.CNVExon {
	_ = now
	type ex struct {
		gene, chrom, typ, transcript string
		start, end                   int64
		exons                        int
	}
	specs := []ex{
		{"DMD", "chrX", "DEL", "NM_004006.3", 31144759, 31148000, 3},
		{"DMD", "chrX", "DEL", "NM_004006.3", 31200000, 31205000, 2},
		{"SCN1A", "chr2", "DUP", "NM_001165963.4", 166848000, 166852000, 1},
		{"BRCA1", "chr17", "DEL", "NM_007294.4", 43044000, 43048000, 2},
		{"PMP22", "chr17", "DUP", "NM_000304.4", 15130000, 15150000, 4},
		{"UBE3A", "chr15", "DEL", "NM_130838.3", 25330000, 25340000, 1},
		{"NRXN1", "chr2", "DEL", "NM_001135659.3", 50100000, 50120000, 2},
		{"TTN", "chr2", "DUP", "NM_001267550.2", 179400000, 179405000, 1},
		{"MECP2", "chrX", "DEL", "NM_004992.4", 153295000, 153298000, 1},
		{"GJB2", "chr13", "Normal", "NM_004004.6", 20763000, 20764000, 1},
		{"CFTR", "chr7", "DEL", "NM_000492.4", 117120000, 117125000, 2},
		{"PAH", "chr12", "DUP", "NM_000277.3", 103230000, 103235000, 1},
	}
	out := make([]model.CNVExon, 0, len(specs))
	for i, s := range specs {
		ratio := 0.5
		if s.typ == "DUP" {
			ratio = 1.5
		} else if s.typ == "Normal" {
			ratio = 1.0
		}
		out = append(out, model.CNVExon{
			ID:             fmt.Sprintf("99999999-9999-4999-8999-%012d", i+1),
			TaskID:         taskCompletedUUID,
			Chromosome:     s.chrom,
			StartPosition:  s.start,
			EndPosition:    s.end,
			Type:           s.typ,
			Gene:           s.gene,
			Transcript:     s.transcript,
			ExonCount:      s.exons,
			CopyRatio:      ptrF64(ratio),
			DepthRatio:     ptrF64(ratio),
			Impact:         "HIGH",
			Classification: "VUS",
			DosageGenes:    s.gene,
			Reason:         "seed demo CNV exon",
		})
	}
	return out
}
