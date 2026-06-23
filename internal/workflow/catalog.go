package workflow

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"

	"github.com/bioinfo/schema-platform/internal/model"
)

// ImageWorkflowDir is the default WDL workflow directory inside the container.
const ImageWorkflowDir = "/opt/schema/schema-germline"

// Definition describes a workflow template.
type Definition struct {
	Name        string
	ShortName   string
	Domain      string
	Workflow    string
	Description string
	Inputs      map[string]interface{}
}

var definitions = map[string]Definition{
	"germline_single": {
		Name:        "germline_single",
		ShortName:   "single",
		Domain:      "germline",
		Workflow:    "SingleWES",
		Description: "Single-sample WES workflow",
		Inputs: mustParseInputs(`{
			"SingleWES.prefix": "proband",
			"SingleWES.read_1": "/mnt/data/test/202511_proband_ILMN_1.fq.gz",
			"SingleWES.read_2": "/mnt/data/test/202511_proband_ILMN_2.fq.gz",
			"SingleWES.fasta": "/mnt/data/database/Homo_sapiens.GRCh37.dna.primary_assembly.fa",
			"SingleWES.bed": "/mnt/data/test/hg19_IDTv1.bed",
			"SingleWES.flank_size": 50,
			"SingleWES.assembly": "GRCh37",
			"SingleWES.ref_dir": "/mnt/data/database",
			"SingleWES.cnvkit_reference": "/mnt/data/test/reference.cnvkit.cnn",
			"SingleWES.sry_sex_cutoff": 20,
			"SingleWES.cnv_bin_size": 20000,
			"SingleWES.cnv_dup_threshold": 2.5,
			"SingleWES.cnv_del_threshold": 1.5,
			"SingleWES.cache_dir": "/mnt/data/database/vep",
			"SingleWES.schema_bundle": "/mnt/data/database/schema_bundle"
		}`),
	},
	"germline_trio": {
		Name:        "germline_trio",
		ShortName:   "trio",
		Domain:      "germline",
		Workflow:    "TrioWES",
		Description: "Trio WES workflow",
		Inputs: mustParseInputs(`{
			"TrioWES.prefix": "proband",
			"TrioWES.meta_info": {
				"members": ["proband", "father", "mother"],
				"read_1": [
					"/mnt/data/test/202511_proband_ILMN_1.fq.gz",
					"/mnt/data/test/202511_father_ILMN_1.fq.gz",
					"/mnt/data/test/202511_mother_ILMN_1.fq.gz"
				],
				"read_2": [
					"/mnt/data/test/202511_proband_ILMN_2.fq.gz",
					"/mnt/data/test/202511_father_ILMN_2.fq.gz",
					"/mnt/data/test/202511_mother_ILMN_2.fq.gz"
				]
			},
			"TrioWES.fasta": "/mnt/data/database/Homo_sapiens.GRCh37.dna.primary_assembly.fa",
			"TrioWES.bed": "/mnt/data/test/hg19_IDTv1.bed",
			"TrioWES.flank_size": 50,
			"TrioWES.ped": "/mnt/data/test/ped.txt",
			"TrioWES.assembly": "GRCh37",
			"TrioWES.ref_dir": "/mnt/data/database",
			"TrioWES.cnvkit_reference": "/mnt/data/test/reference.cnvkit.cnn",
			"TrioWES.sry_sex_cutoff": 20,
			"TrioWES.cnv_bin_size": 20000,
			"TrioWES.cnv_dup_threshold": 2.5,
			"TrioWES.cnv_del_threshold": 1.5,
			"TrioWES.cache_dir": "/mnt/data/database/vep",
			"TrioWES.schema_bundle": "/mnt/data/database/schema_bundle"
		}`),
	},
	"germline_baseline": {
		Name:        "germline_baseline",
		ShortName:   "baseline",
		Domain:      "germline",
		Workflow:    "CNVBaseline",
		Description: "CNV baseline workflow",
		Inputs: mustParseInputs(`{
			"CNVBaseline.prefix": "reference",
			"CNVBaseline.bed": "/mnt/data/test/hg19_IDTv1.bed",
			"CNVBaseline.fasta": "/mnt/data/database/Homo_sapiens.GRCh37.dna.primary_assembly.fa",
			"CNVBaseline.assembly": "GRCh37",
			"CNVBaseline.read_1": [
				"/mnt/data/test/202511_father_ILMN_1.fq.gz",
				"/mnt/data/test/202511_mother_ILMN_1.fq.gz"
			],
			"CNVBaseline.read_2": [
				"/mnt/data/test/202511_father_ILMN_2.fq.gz",
				"/mnt/data/test/202511_mother_ILMN_2.fq.gz"
			],
			"CNVBaseline.ref_dir": "/mnt/data/database"
		}`),
	},
}

func mustParseInputs(raw string) map[string]interface{} {
	var inputs map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &inputs); err != nil {
		panic(err)
	}
	return inputs
}

// ListDefinitions returns all workflow definitions sorted by name.
func ListDefinitions() []Definition {
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]Definition, 0, len(names))
	for _, name := range names {
		result = append(result, cloneDefinition(definitions[name]))
	}
	return result
}

// GetDefinition returns a workflow definition by name.
func GetDefinition(name string) (Definition, bool) {
	def, ok := definitions[name]
	if !ok {
		return Definition{}, false
	}
	return cloneDefinition(def), true
}

// IsSupported returns true if the template name is a known workflow.
func IsSupported(name string) bool {
	_, ok := definitions[name]
	return ok
}

// WDLPath returns the path to the WDL file for a given template name.
func WDLPath(templateDir, name string) (string, error) {
	def, ok := GetDefinition(name)
	if !ok {
		return "", fmt.Errorf("unsupported template: %s", name)
	}
	return path.Join(templateDir, def.ShortName+".wdl"), nil
}

// ConfigPath returns the path to the executor config file.
func ConfigPath(templateDir string, executor model.ExecutorType) string {
	cfgName := "local.cfg"
	switch executor {
	case model.ExecutorSlurm:
		cfgName = "slurm.cfg"
	case model.ExecutorLSF:
		cfgName = "lsf.cfg"
	}
	return path.Join(templateDir, "conf", cfgName)
}

// ToTemplate converts a Definition to an API Template response.
func ToTemplate(templateDir string, def Definition, includeInputs bool) model.Template {
	tmpl := model.Template{
		Name:        def.Name,
		ShortName:   def.ShortName,
		Domain:      def.Domain,
		Workflow:    def.Workflow,
		Path:        path.Join(templateDir, def.ShortName+".wdl"),
		Description: def.Description,
		InputFields: inputFields(def.Inputs),
	}
	if includeInputs {
		tmpl.DefaultInputs = CloneInputs(def.Inputs)
	}
	return tmpl
}

func inputFields(inputs map[string]interface{}) []string {
	fields := make([]string, 0, len(inputs))
	for field := range inputs {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

func cloneDefinition(def Definition) Definition {
	def.Inputs = CloneInputs(def.Inputs)
	return def
}

// CloneInputs deep-clones an input map via JSON round-trip.
func CloneInputs(inputs map[string]interface{}) map[string]interface{} {
	raw, err := json.Marshal(inputs)
	if err != nil {
		return map[string]interface{}{}
	}
	var cloned map[string]interface{}
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return map[string]interface{}{}
	}
	return cloned
}
