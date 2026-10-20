package service

import (
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestDeclaredReferenceIDUsesInputSnapshot(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{name: "hg19", json: `{"reference_genome":"hg19"}`, want: "hg19"},
		{name: "nested grch37", json: `{"workflow":{"assembly":"GRCh37"}}`, want: "hg19"},
		{name: "hg38 fasta", json: `{"inputs":{"fasta":"/refs/GRCh38.fa"}}`, want: "hg38"},
		{name: "VCF qualified assembly", json: `{"VCF2Anno.assembly":"GRCh37","VCF2Anno.fasta":"/refs/reference.fa"}`, want: "hg19"},
		{name: "qualified hg38 assembly", json: `{"VCF2Anno.assembly":"GRCh38","VCF2Anno.fasta":"/refs/GRCh37.fa"}`, want: "hg38"},
		{name: "unknown", json: `{"assembly":"T2T-CHM13"}`, want: ""},
		{name: "invalid", json: `not-json`, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := declaredReferenceID(test.json); got != test.want {
				t.Fatalf("declaredReferenceID() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTaskReferenceUsesFrozenPipelineWhenInputsHaveNoDeclaration(t *testing.T) {
	tests := []struct{ inputs, snapshot, want string }{
		{`{}`, `{"reference":"hg19"}`, "hg19"},
		{`{"VCF2Anno.fasta":"/refs/reference.fa"}`, `{"reference":"hg38"}`, "hg38"},
		{`{"VCF2Anno.assembly":"T2T-CHM13"}`, `{"reference":"hg19"}`, ""},
		{`{"VCF2Anno.assembly":"GRCh38"}`, `{"reference":"hg19"}`, "hg38"},
		{`{}`, `{"reference":"hg38"}`, "hg38"},
	}
	for _, test := range tests {
		task := &model.Task{InputJSON: test.inputs, PipelineSnapshotJSON: test.snapshot}
		if got := taskReferenceID(task); got != test.want {
			t.Fatalf("taskReferenceID(%s, %s)=%q, want %q", test.inputs, test.snapshot, got, test.want)
		}
	}
}

func TestQCMetricsKeepsFocusedZeroDistinctFromUnavailable(t *testing.T) {
	metrics := qcMetrics(model.QCResult{
		TotalReads:         0,
		AverageDepth:       0,
		MetricAvailability: `{"totalReads":true,"averageDepth":false}`,
	})
	byKey := make(map[string]model.QCMetric, len(metrics))
	for _, metric := range metrics {
		byKey[metric.Key] = metric
	}
	if byKey["totalReads"].Value == nil || *byKey["totalReads"].Value != 0 {
		t.Fatalf("focused zero totalReads was not preserved: %#v", byKey["totalReads"].Value)
	}
	if byKey["averageDepth"].Value != nil {
		t.Fatalf("unavailable averageDepth was rendered as a value: %#v", byKey["averageDepth"].Value)
	}
}

func TestResultMembersAndQCSortsTrioRoles(t *testing.T) {
	members, _ := resultMembersAndQC([]model.QCResult{
		{MemberID: "mother-id", MemberRole: "mother"},
		{MemberID: "father-id", MemberRole: "father"},
		{MemberID: "proband-id", MemberRole: "proband"},
	})
	if len(members) != 3 || members[0].Role != "proband" || members[1].Role != "father" || members[2].Role != "mother" {
		t.Fatalf("unexpected member order: %#v", members)
	}
}
