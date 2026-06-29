package service

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/bioinfo/schema-platform/internal/repository"
	"github.com/google/uuid"
)

// Importer handles importing archived data into the database
type Importer struct {
	cfg  *config.Config
	repo *repository.ResultRepository
}

// NewImporter creates a new importer
func NewImporter(cfg *config.Config) *Importer {
	return &Importer{
		cfg:  cfg,
		repo: repository.NewResultRepository(),
	}
}

// ImportResult represents the result of an import operation
type ImportResult struct {
	UUID        string         `json:"uuid"`
	Success     bool           `json:"success"`
	Counts      map[string]int `json:"counts"`
	SourceFiles []string       `json:"source_files,omitempty"`
	Error       string         `json:"error,omitempty"`
}

// ImportFromTaskArchive imports structure data from a task archive.
func (imp *Importer) ImportFromTaskArchive(task *model.Task, archiveDir string) (*ImportResult, error) {
	if task == nil {
		return nil, fmt.Errorf("task is required")
	}
	return imp.ImportFromArchive(task.UUID, archiveDir)
}

// ImportFromArchive imports all result data from an archive directory into the database
func (imp *Importer) ImportFromArchive(taskID string, archiveDir string) (*ImportResult, error) {
	result := &ImportResult{
		UUID:    taskID,
		Success: false,
		Counts:  make(map[string]int),
	}

	// 1. Read and import QC from outputs.resolved.json
	if err := imp.importQC(taskID, archiveDir, result); err != nil {
		fmt.Printf("warning: failed to import QC: %v\n", err)
	}

	// 2. Find result TSV files in the archive directory
	tsvFiles := imp.findResultFiles(archiveDir)
	result.SourceFiles = append(result.SourceFiles, filepath.Join(archiveDir, "outputs.resolved.json"))
	result.SourceFiles = append(result.SourceFiles, tsvFiles...)

	// 3. Import each file
	for _, tsvPath := range tsvFiles {
		fileName := filepath.Base(tsvPath)

		switch {
		case strings.Contains(fileName, "snv_indel") || strings.Contains(fileName, "snv.indel"):
			count, err := imp.importSNVIndels(taskID, tsvPath)
			if err != nil {
				fmt.Printf("warning: failed to import %s: %v\n", fileName, err)
				continue
			}
			result.Counts["snv_indel"] = count

		case strings.Contains(fileName, "region.cnvanno"):
			count, err := imp.importCNVSegments(taskID, tsvPath)
			if err != nil {
				fmt.Printf("warning: failed to import %s: %v\n", fileName, err)
				continue
			}
			result.Counts["cnv_segment"] = count

		case strings.Contains(fileName, "gene.cnvanno"):
			count, err := imp.importCNVExons(taskID, tsvPath)
			if err != nil {
				fmt.Printf("warning: failed to import %s: %v\n", fileName, err)
				continue
			}
			result.Counts["cnv_exon"] = count

		case strings.Contains(fileName, ".str") || strings.HasSuffix(fileName, "str.txt"):
			count, err := imp.importSTRs(taskID, tsvPath)
			if err != nil {
				fmt.Printf("warning: failed to import %s: %v\n", fileName, err)
				continue
			}
			result.Counts["str"] = count

		case strings.Contains(fileName, ".mei") || strings.HasSuffix(fileName, "mei.txt"):
			count, err := imp.importMEIs(taskID, tsvPath)
			if err != nil {
				fmt.Printf("warning: failed to import %s: %v\n", fileName, err)
				continue
			}
			result.Counts["mei"] = count

		case strings.Contains(fileName, "mt_report") || strings.Contains(fileName, ".mt"):
			count, err := imp.importMTVariants(taskID, tsvPath)
			if err != nil {
				fmt.Printf("warning: failed to import %s: %v\n", fileName, err)
				continue
			}
			result.Counts["mt"] = count

		case strings.Contains(fileName, "roh"):
			count, err := imp.importROHRegions(taskID, tsvPath)
			if err != nil {
				fmt.Printf("warning: failed to import %s: %v\n", fileName, err)
				continue
			}
			result.Counts["roh"] = count

		default:
			fmt.Printf("skipping unknown file: %s\n", fileName)
		}
	}

	result.Success = true
	return result, nil
}

// importQC reads QC data from outputs.resolved.json
func (imp *Importer) importQC(taskID string, archiveDir string, result *ImportResult) error {
	resolvedPath := filepath.Join(archiveDir, "outputs.resolved.json")
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return err
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}

	// Find qc_result in any summary
	var qcData map[string]interface{}
	for _, v := range parsed {
		if summary, ok := v.(map[string]interface{}); ok {
			if qc, ok := summary["qc_result"].(map[string]interface{}); ok {
				qcData = qc
				break
			}
		}
	}

	if qcData == nil {
		return fmt.Errorf("qc_result not found in outputs.resolved.json")
	}

	// Delete existing QC for this task
	imp.repo.DeleteQCByTaskID(taskID)

	qc := imp.parseQCResult(taskID, qcData)
	if err := imp.repo.CreateQC(qc); err != nil {
		return err
	}
	result.Counts["qc"] = 1
	return nil
}

// parseQCResult parses the nested qc_result JSON into a flat QCResult model
func (imp *Importer) parseQCResult(taskID string, qc map[string]interface{}) *model.QCResult {
	r := &model.QCResult{
		ID:     uuid.New().String(),
		TaskID: taskID,
	}

	if sid, ok := qc["sample_id"].(string); ok {
		r.SampleID = sid
	}

	// fastp.after_filtering
	if fastp, ok := qc["fastp"].(map[string]interface{}); ok {
		if af, ok := fastp["after_filtering"].(map[string]interface{}); ok {
			r.TotalReads = int64(getInt(af, "total_reads"))
			r.TotalBases = int64(getInt(af, "total_bases"))
			r.Q20Rate = getFloat(af, "q20_rate")
			r.Q30Rate = getFloat(af, "q30_rate")
			r.GcContent = getFloat(af, "gc_content")
			r.Read1MeanLength = getInt(af, "read1_mean_length")
			r.Read2MeanLength = getInt(af, "read2_mean_length")
		}
	}

	// xamdst
	if xd, ok := qc["xamdst"].(map[string]interface{}); ok {
		r.AverageDepth = getFloat(xd, "average_depth")
		r.DedupDepth = getFloat(xd, "average_depth_rmdup")
		r.CoverageGt0x = getFloat(xd, "coverage_gt_0x")
		r.CoverageGte30x = getFloat(xd, "coverage_gte_30x")
		r.CoverageGte100x = getFloat(xd, "coverage_gte_100x")
		r.MappedReads = int64(getInt(xd, "mapped_reads"))
		r.MappedReadsFraction = getFloat(xd, "mapped_reads_fraction")
		r.InsertSizeAverage = getFloat(xd, "insert_size_average")
		r.InsertSizeMedian = getInt(xd, "insert_size_median")
		r.RegionLength = int64(getInt(xd, "region_length"))
		r.TargetDataFraction = getFloat(xd, "target_data_fraction_all")
	}

	// hs_metrics
	if hs, ok := qc["hs_metrics"].(map[string]interface{}); ok {
		r.MeanTargetCoverage = getFloat(hs, "mean_target_coverage")
		r.MedianTargetCoverage = getInt(hs, "median_target_coverage")
		r.PctTargetBases30x = getFloat(hs, "pct_target_bases_30x")
		r.PctTargetBases100x = getFloat(hs, "pct_target_bases_100x")
		r.FoldEnrichment = getFloat(hs, "fold_enrichment")
		r.ZeroCvgTargetsPct = getFloat(hs, "zero_cvg_targets_pct")
	}

	// sambamba
	if sb, ok := qc["sambamba"].(map[string]interface{}); ok {
		r.DuplicateRate = getFloat(sb, "percent_duplication")
	}

	// sry
	if sry, ok := qc["sry"].(map[string]interface{}); ok {
		if g, ok := sry["predicted_gender"].(string); ok {
			r.PredictedGender = g
		}
		r.SryCount = getInt(sry, "sry_count")
	}

	// mt_xamdst
	if mxd, ok := qc["mt_xamdst"].(map[string]interface{}); ok {
		r.MtAverageDepth = getFloat(mxd, "mt_average_depth")
		r.MtCoverageGt0x = getFloat(mxd, "mt_coverage_gt_0x")
	}

	// fingerprint
	if fp, ok := qc["fingerprint"].(map[string]interface{}); ok {
		if h, ok := fp["fingerprint_hash"].(string); ok {
			r.FingerprintHash = h
		}
	}

	// metrics
	if m, ok := qc["metrics"].(map[string]interface{}); ok {
		r.PfMismatchRate = getFloat(m, "pf_mismatch_rate")
	}

	return r
}

// findResultFiles finds all result TSV files in the archive directory
func (imp *Importer) findResultFiles(archiveDir string) []string {
	var files []string

	// Walk archive dir and subdirectories
	filepath.Walk(archiveDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		name := strings.ToLower(info.Name())
		// Match result files
		if strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".tsv") {
			if strings.Contains(name, "snv_indel") || strings.Contains(name, "snv.indel") ||
				strings.Contains(name, "region.cnvanno") || strings.Contains(name, "gene.cnvanno") ||
				strings.Contains(name, ".str") || strings.Contains(name, "str.txt") ||
				strings.Contains(name, ".mei") || strings.Contains(name, "mei.txt") ||
				strings.Contains(name, "mt_report") || strings.Contains(name, ".mt_") ||
				strings.Contains(name, "roh") {
				files = append(files, path)
			}
		}

		return nil
	})

	return files
}

// readTSV reads a TSV file and returns headers + rows
func readTSV(path string) (headers []string, rows []map[string]string, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = '\t'
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // Allow variable number of fields

	// Read header
	headers, err = reader.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read header: %w", err)
	}

	// Trim whitespace from headers
	for i, h := range headers {
		headers[i] = strings.TrimSpace(h)
	}

	// Read data rows
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue // Skip malformed rows
		}

		row := make(map[string]string)
		for i, h := range headers {
			if i < len(record) {
				row[h] = strings.TrimSpace(record[i])
			} else {
				row[h] = ""
			}
		}
		rows = append(rows, row)
	}

	return headers, rows, nil
}

// importSNVIndels imports SNV/Indel data
func (imp *Importer) importSNVIndels(taskID string, path string) (int, error) {
	_, rows, err := readTSV(path)
	if err != nil {
		return 0, err
	}

	imp.repo.DeleteSNVIndelsByTaskID(taskID)

	var results []model.SNVIndel
	for _, row := range rows {
		v := model.SNVIndel{
			ID:                  uuid.New().String(),
			TaskID:              taskID,
			Chromosome:          row["Chromosome"],
			Position:            parseInt64(row["Position"]),
			VariantID:           row["Variant_ID"],
			Ref:                 row["Ref"],
			Alt:                 row["Alt"],
			VariantType:         row["Type"],
			Quality:             parseFloat(row["Quality"]),
			Filter:              row["Filter"],
			Genotype:            row["Genotype"],
			Zygosity:            row["Zygosity"],
			PhaseSet:            row["PhaseSet"],
			Depth:               parseInt(row["Depth"]),
			AD:                  row["AD"],
			VAF:                 parseFloat(row["VAF"]),
			Gene:                row["Gene"],
			Transcript:          row["Transcript"],
			Location:            row["Location"],
			Consequence:         row["Consequence"],
			Impact:              row["Impact"],
			HGVSc:               row["HGVS_c"],
			HGVSp:               row["HGVS_p"],
			AminoAcids:          row["Amino_Acids"],
			Cytoband:            row["Cytoband"],
			ClinvarSignificance: row["ClinVar_Sig"],
			ClinvarRevStat:      row["ClinVar_RevStat"],
			ClinvarDN:           row["ClinVar_DN"],
			ClinvarStar:         row["ClinVar_Star"],
			GnomadAF:            parseFloatPtr(row["GnomAD_AF"]),
			GnomadEasAF:         parseFloatPtr(row["GnomAD_AF_EAS"]),
			GnomadNhomaltXX:     parseFloatPtr(row["GnomAD_nhomalt_XX"]),
			GnomadNhomaltXY:     parseFloatPtr(row["GnomAD_nhomalt_XY"]),
			PangolinGain:        parseFloatPtr(row["Pangolin_Gain"]),
			PangolinLoss:        parseFloatPtr(row["Pangolin_Loss"]),
			PangolinAN:          parseFloatPtr(row["Pangolin_AN"]),
			EVOScore:            parseFloatPtr(row["EVOScore"]),
			EVOScoreAN:          parseFloatPtr(row["EVOScore_AN"]),
			AlphaMissenseAM:     parseFloatPtr(row["AlphaMissense_AM"]),
			AlphaMissenseAMC:    row["AlphaMissense_AMC"],
			HgncID:              row["HGNC_ID"],
			RsID:                row["dbSNP"],
			MaxAF:               parseFloatPtr(row["MAX_AF"]),
			GenccMoi:            row["GenCC_moi_curie"],
			GenccDiseaseTitle:   row["GenCC_disease_title"],
			GenccMoiTitle:       row["GenCC_moi_title"],
		}
		results = append(results, v)
	}

	if err := imp.repo.CreateSNVIndels(results); err != nil {
		return 0, err
	}
	return len(results), nil
}

// importCNVSegments imports CNV region-level data
func (imp *Importer) importCNVSegments(taskID string, path string) (int, error) {
	_, rows, err := readTSV(path)
	if err != nil {
		return 0, err
	}

	imp.repo.DeleteCNVSegmentsByTaskID(taskID)

	var results []model.CNVSegment
	for _, row := range rows {
		v := model.CNVSegment{
			ID:                   uuid.New().String(),
			TaskID:               taskID,
			Chromosome:           row["Chromosome"],
			StartPosition:        parseInt64(row["Start"]),
			EndPosition:          parseInt64(row["End"]),
			Type:                 row["Col4"],
			Log2Ratio:            parseFloatPtr(row["Col5"]),
			Depth:                parseFloatPtr(row["Col6"]),
			Weight:               parseFloatPtr(row["Col7"]),
			CopyRatio:            parseFloatPtr(row["Col8"]),
			ISCN:                 row["ISCN"],
			GeneCount:            parseInt(row["Gene_Count"]),
			HIMax:                parseFloatPtr(row["HI_Max"]),
			TRMax:                parseFloatPtr(row["TR_Max"]),
			MaxFrequency:         parseFloatPtr(row["Max_Frequency"]),
			Section1:             parseFloatPtr(row["Section1"]),
			Section2:             parseFloatPtr(row["Section2"]),
			Section3:             parseFloatPtr(row["Section3"]),
			Section4:             parseFloatPtr(row["Section4"]),
			Section5:             parseFloatPtr(row["Section5"]),
			TotalScore:           parseFloatPtr(row["Total_Score"]),
			Evidence1A:           parseFloatPtr(row["Evidence_1A"]),
			Evidence1B:           parseFloatPtr(row["Evidence_1B"]),
			Evidence2A:           parseFloatPtr(row["Evidence_2A"]),
			Evidence2B:           parseFloatPtr(row["Evidence_2B"]),
			Evidence2C:           parseFloatPtr(row["Evidence_2C"]),
			Evidence2D:           parseFloatPtr(row["Evidence_2D"]),
			Evidence2E:           parseFloatPtr(row["Evidence_2E"]),
			Evidence2F:           parseFloatPtr(row["Evidence_2F"]),
			Evidence2H:           parseFloatPtr(row["Evidence_2H"]),
			Evidence2K:           parseFloatPtr(row["Evidence_2K"]),
			Evidence3:            parseFloatPtr(row["Evidence_3"]),
			Evidence4O:           parseFloatPtr(row["Evidence_4O"]),
			Evidence4A:           parseFloatPtr(row["Evidence_4A"]),
			Evidence4L:           parseFloatPtr(row["Evidence_4L"]),
			Evidence5:            parseFloatPtr(row["Evidence_5"]),
			DosageGenes:          row["Dosage_Genes"],
			PathogenicRegions:    row["Pathogenic_Regions"],
			BenignRegionsOverlap: row["Benign_Regions_Overlap"],
			GenCCADGenes:         row["GenCC_AD_Genes"],
			Classification:       row["Classification"],
			Reason:               row["Reason"],
			EvidenceDetails:      row["Evidence_Details"],
		}
		results = append(results, v)
	}

	if err := imp.repo.CreateCNVSegments(results); err != nil {
		return 0, err
	}
	return len(results), nil
}

// importCNVExons imports CNV gene-level data
func (imp *Importer) importCNVExons(taskID string, path string) (int, error) {
	_, rows, err := readTSV(path)
	if err != nil {
		return 0, err
	}

	imp.repo.DeleteCNVExonsByTaskID(taskID)

	var results []model.CNVExon
	for _, row := range rows {
		v := model.CNVExon{
			ID:                   uuid.New().String(),
			TaskID:               taskID,
			Chromosome:           row["Chromosome"],
			StartPosition:        parseInt64(row["Start"]),
			EndPosition:          parseInt64(row["End"]),
			Type:                 row["Col4"],
			Gene:                 row["Col5"],
			Transcript:           row["Col6"],
			EnsemblTranscript:    row["Col7"],
			ExonCount:            parseInt(row["Col8"]),
			Log2Ratio:            parseFloatPtr(row["Col9"]),
			CopyRatio:            parseFloatPtr(row["Col10"]),
			Weight:               parseFloatPtr(row["Col11"]),
			DepthRatio:           parseFloatPtr(row["Col12"]),
			Depth:                parseFloatPtr(row["Col13"]),
			Quality:              parseFloatPtr(row["Col14"]),
			Ratio2:               parseFloatPtr(row["Col15"]),
			Flag1:                parseInt(row["Col16"]),
			Flag2:                parseInt(row["Col17"]),
			FloatFlag:            parseFloatPtr(row["Col18"]),
			Impact:               row["Col19"],
			ISCN:                 row["ISCN"],
			GeneCount:            parseInt(row["Gene_Count"]),
			HIMax:                parseFloatPtr(row["HI_Max"]),
			TRMax:                parseFloatPtr(row["TR_Max"]),
			MaxFrequency:         parseFloatPtr(row["Max_Frequency"]),
			Section1:             parseFloatPtr(row["Section1"]),
			Section2:             parseFloatPtr(row["Section2"]),
			Section3:             parseFloatPtr(row["Section3"]),
			Section4:             parseFloatPtr(row["Section4"]),
			Section5:             parseFloatPtr(row["Section5"]),
			TotalScore:           parseFloatPtr(row["Total_Score"]),
			Evidence1A:           parseFloatPtr(row["Evidence_1A"]),
			Evidence1B:           parseFloatPtr(row["Evidence_1B"]),
			Evidence2A:           parseFloatPtr(row["Evidence_2A"]),
			Evidence2B:           parseFloatPtr(row["Evidence_2B"]),
			Evidence2C:           parseFloatPtr(row["Evidence_2C"]),
			Evidence2D:           parseFloatPtr(row["Evidence_2D"]),
			Evidence2E:           parseFloatPtr(row["Evidence_2E"]),
			Evidence2F:           parseFloatPtr(row["Evidence_2F"]),
			Evidence2H:           parseFloatPtr(row["Evidence_2H"]),
			Evidence2K:           parseFloatPtr(row["Evidence_2K"]),
			Evidence3:            parseFloatPtr(row["Evidence_3"]),
			Evidence4O:           parseFloatPtr(row["Evidence_4O"]),
			Evidence4A:           parseFloatPtr(row["Evidence_4A"]),
			Evidence4L:           parseFloatPtr(row["Evidence_4L"]),
			Evidence5:            parseFloatPtr(row["Evidence_5"]),
			DosageGenes:          row["Dosage_Genes"],
			PathogenicRegions:    row["Pathogenic_Regions"],
			BenignRegionsOverlap: row["Benign_Regions_Overlap"],
			GenCCADGenes:         row["GenCC_AD_Genes"],
			Classification:       row["Classification"],
			Reason:               row["Reason"],
			EvidenceDetails:      row["Evidence_Details"],
		}
		results = append(results, v)
	}

	if err := imp.repo.CreateCNVExons(results); err != nil {
		return 0, err
	}
	return len(results), nil
}

// importSTRs imports STR data
func (imp *Importer) importSTRs(taskID string, path string) (int, error) {
	_, rows, err := readTSV(path)
	if err != nil {
		return 0, err
	}

	imp.repo.DeleteSTRsByTaskID(taskID)

	var results []model.STR
	for _, row := range rows {
		v := model.STR{
			ID:             uuid.New().String(),
			TaskID:         taskID,
			Chromosome:     row["Chromosome"],
			Position:       parseInt64(row["Position"]),
			Gene:           row["Gene"],
			RepeatUnit:     row["Repeat_Unit"],
			RefRepeats:     parseInt(row["Ref_Repeats"]),
			Allele1Repeats: row["Allele1_Repeats"],
			Allele2Repeats: row["Allele2_Repeats"],
			RepeatDisplay:  row["Repeat_Display"],
			Status:         row["STR_Status"],
			Pathogenicity:  row["Pathogenicity"],
			NormalRangeMax: parseInt(row["Normal_Max"]),
			PathologicMin:  parseInt(row["Pathologic_Min"]),
			Disease:        row["Disease"],
			Inheritance:    row["Inheritance"],
			HgncID:         row["HGNC_ID"],
			Depth:          parseFloat(row["Depth"]),
			SpanningReads:  row["Spanning_Reads"],
			FlankingReads:  row["Flanking_Reads"],
			InRepeatReads:  row["InRepeat_Reads"],
			SwegenMean:     parseFloatPtr(row["SweGen_Mean"]),
			SwegenStd:      parseFloatPtr(row["SweGen_Std"]),
			Source:         row["Source"],
			Filter:         row["Filter"],
		}
		results = append(results, v)
	}

	if err := imp.repo.CreateSTRs(results); err != nil {
		return 0, err
	}
	return len(results), nil
}

// importMEIs imports MEI data
func (imp *Importer) importMEIs(taskID string, path string) (int, error) {
	_, rows, err := readTSV(path)
	if err != nil {
		return 0, err
	}

	imp.repo.DeleteMEIsByTaskID(taskID)

	var results []model.MEIVariant
	for _, row := range rows {
		v := model.MEIVariant{
			ID:                uuid.New().String(),
			TaskID:            taskID,
			Chromosome:        row["Chromosome"],
			Position:          parseInt64(row["Position"]),
			MEIID:             row["MEI_ID"],
			TEType:            row["TE_Type"],
			TEFamily:          row["TE_Family"],
			Direction:         row["Direction"],
			Confidence:        row["Confidence"],
			SupportingReads:   parseInt(row["Support_Reads"]),
			AvgSoftClipLength: parseFloat(row["Avg_SoftClip_Length"]),
			Gene:              row["Gene"],
			Transcript:        row["Transcript"],
			Location:          row["Location"],
			Consequence:       row["Consequence"],
			Impact:            row["Impact"],
			Cytoband:          row["Cytoband"],
			ClinvarSig:        row["ClinVar_Sig"],
			ClinvarDN:         row["ClinVar_DN"],
			ClinvarStar:       row["ClinVar_Star"],
			GnomadAF:          parseFloatPtr(row["GnomAD_AF"]),
			HgncID:            row["HGNC_ID"],
			Filter:            row["Filter"],
		}
		results = append(results, v)
	}

	if err := imp.repo.CreateMEIs(results); err != nil {
		return 0, err
	}
	return len(results), nil
}

// importMTVariants imports mitochondrial variant data
func (imp *Importer) importMTVariants(taskID string, path string) (int, error) {
	_, rows, err := readTSV(path)
	if err != nil {
		return 0, err
	}

	imp.repo.DeleteMTVariantsByTaskID(taskID)

	var results []model.MitochondrialVariant
	for _, row := range rows {
		v := model.MitochondrialVariant{
			ID:                 uuid.New().String(),
			TaskID:             taskID,
			Chromosome:         row["Chromosome"],
			Position:           parseInt64(row["Position"]),
			MTGene:             row["MT_Gene"],
			MTGeneType:         row["MT_Gene_Type"],
			MitophenVariant:    row["Mitophen_Variant"],
			MitophenPhenotypes: row["Mitophen_Phenotypes"],
			MTHGVS:             row["MT_HGVS"],
			Ref:                row["Ref"],
			Alt:                row["Alt"],
			VariantType:        row["Type"],
			Filter:             row["Filter"],
			Genotype:           row["Genotype"],
			Heteroplasmy:       parseFloat(row["Heteroplasmy"]),
			HeteroplasmyClass:  row["Heteroplasmy_Class"],
			Depth:              parseInt(row["Depth"]),
			AD:                 row["AD"],
			AF:                 row["AF"],
			Gene:               row["Gene"],
			Feature:            row["Feature"],
			Consequence:        row["Consequence"],
			Impact:             row["Impact"],
			HGVS_c:             row["HGVS_c"],
			HGVS_p:             row["HGVS_p"],
			AminoAcids:         row["Amino_Acids"],
			ProteinPosition:    row["Protein_Position"],
			ClinvarSig:         row["ClinVar_Sig"],
			ClinvarDN:          row["ClinVar_DN"],
			ClinvarStar:        row["ClinVar_Star"],
			GnomadAF:           parseFloatPtr(row["GnomAD_AF"]),
			GnomadEasAF:        parseFloatPtr(row["GnomAD_AF_EAS"]),
			DbSNP:              row["dbSNP"],
			MaxAF:              parseFloatPtr(row["MAX_AF"]),
			TLOD:               row["TLOD"],
			POPAF:              row["POPAF"],
			GERMQ:              row["GERMQ"],
			STRANDQ:            row["STRANDQ"],
			CONTQ:              row["CONTQ"],
			SEQQ:               row["SEQQ"],
			MBQ:                row["MBQ"],
			MMQ:                row["MMQ"],
			MFRL:               row["MFRL"],
		}
		results = append(results, v)
	}

	if err := imp.repo.CreateMTVariants(results); err != nil {
		return 0, err
	}
	return len(results), nil
}

// importROHRegions imports ROH data
func (imp *Importer) importROHRegions(taskID string, path string) (int, error) {
	_, rows, err := readTSV(path)
	if err != nil {
		return 0, err
	}

	imp.repo.DeleteROHRegionsByTaskID(taskID)

	var results []model.ROHRegion
	for _, row := range rows {
		v := model.ROHRegion{
			ID:                     uuid.New().String(),
			TaskID:                 taskID,
			Chr:                    row["Chr"],
			Begin:                  parseInt64(row["Begin"]),
			End:                    parseInt64(row["End"]),
			SizeMb:                 parseFloat(row["Size(Mb)"]),
			NbVariants:             parseInt(row["Nb_variants"]),
			PercentageHomozygosity: parseFloat(row["Percentage_homozygosity"]),
			RecessiveGenes:         row["Recessive_Genes"],
			GeneCount:              parseInt(row["Gene_Count"]),
		}
		results = append(results, v)
	}

	if err := imp.repo.CreateROHRegions(results); err != nil {
		return 0, err
	}
	return len(results), nil
}

// Helper functions for parsing

func parseInt(s string) int {
	if s == "" || s == "." {
		return 0
	}
	v, _ := strconv.Atoi(s)
	return v
}

func parseInt64(s string) int64 {
	if s == "" || s == "." {
		return 0
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func parseFloat(s string) float64 {
	if s == "" || s == "." {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseFloatPtr(s string) *float64 {
	if s == "" || s == "." {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func getFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case float32:
			return float64(val)
		case int:
			return float64(val)
		case int64:
			return float64(val)
		case json.Number:
			f, _ := val.Float64()
			return f
		}
	}
	return 0
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return int(val)
		case int:
			return val
		case int64:
			return int(val)
		case json.Number:
			i, _ := val.Int64()
			return int(i)
		}
	}
	return 0
}
