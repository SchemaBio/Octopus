package workflow

import "testing"

func TestInputsForGenomeUsesMatchingDefaultCNVBaseline(t *testing.T) {
	tests := []struct {
		name      string
		template  string
		genome    string
		inputKey  string
		wantValue string
	}{
		{
			name:      "single hg19",
			template:  "single",
			genome:    "hg19",
			inputKey:  "SingleWES.cnvkit_reference",
			wantValue: "/mnt/data/database/schema_bundle/hg19_reference.cnvkit.cnn",
		},
		{
			name:      "single hg38",
			template:  "single",
			genome:    "hg38",
			inputKey:  "SingleWES.cnvkit_reference",
			wantValue: "/mnt/data/database/schema_bundle/hg38_reference.cnvkit.cnn",
		},
		{
			name:      "trio hg19",
			template:  "trio",
			genome:    "GRCh37",
			inputKey:  "TrioWES.cnvkit_reference",
			wantValue: "/mnt/data/database/schema_bundle/hg19_reference.cnvkit.cnn",
		},
		{
			name:      "trio hg38",
			template:  "trio",
			genome:    "GRCh38",
			inputKey:  "TrioWES.cnvkit_reference",
			wantValue: "/mnt/data/database/schema_bundle/hg38_reference.cnvkit.cnn",
		},
		{
			name:      "baseline fix hg19",
			template:  "baseline_fix",
			genome:    "hg19",
			inputKey:  "CNVBaselineFix.existing_reference",
			wantValue: "/mnt/data/database/schema_bundle/hg19_reference.cnvkit.cnn",
		},
		{
			name:      "baseline fix hg38",
			template:  "baseline_fix",
			genome:    "hg38",
			inputKey:  "CNVBaselineFix.existing_reference",
			wantValue: "/mnt/data/database/schema_bundle/hg38_reference.cnvkit.cnn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs, err := InputsForGenome(tt.template, tt.genome)
			if err != nil {
				t.Fatalf("InputsForGenome(%q, %q): %v", tt.template, tt.genome, err)
			}
			if got := inputs[tt.inputKey]; got != tt.wantValue {
				t.Fatalf("%s = %#v, want %q", tt.inputKey, got, tt.wantValue)
			}
		})
	}
}
