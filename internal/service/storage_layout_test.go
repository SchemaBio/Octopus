package service

import (
	"reflect"
	"testing"

	"github.com/SchemaBio/Octopus/internal/model"
)

func TestTenantUploadObjectKey(t *testing.T) {
	orgID := "f83165ee-e23c-4bf1-a42e-78ac39c6f1ba"
	jobID := "85bf15f4-77ee-4ddb-a09f-31b00bb29ad8"
	tests := []struct {
		name     string
		fileType model.UploadFileType
		genome   string
		want     string
	}{
		{"paired FASTQ", model.UploadFileTypeFastqPaired, "", "organizations/" + orgID + "/uploads/fastq/paired/" + jobID + "/reads.fastq.gz"},
		{"single FASTQ", model.UploadFileTypeFastqSingle, "", "organizations/" + orgID + "/uploads/fastq/single/" + jobID + "/reads.fastq.gz"},
		{"GRCh37 BED", model.UploadFileTypeBed, "hg19", "organizations/" + orgID + "/uploads/bed/GRCh37/" + jobID + "/reads.fastq.gz"},
		{"other", model.UploadFileTypeOther, "", "organizations/" + orgID + "/uploads/other/" + jobID + "/reads.fastq.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tenantUploadObjectKey(orgID, jobID, tt.fileType, tt.genome, "reads.fastq.gz")
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("unexpected object key: %q", got)
			}
		})
	}
}

func TestTenantDirectoryMarkers(t *testing.T) {
	orgID := "f83165ee-e23c-4bf1-a42e-78ac39c6f1ba"
	got, err := tenantDirectoryMarkers(orgID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"organizations/" + orgID + "/uploads/fastq/paired/.keep",
		"organizations/" + orgID + "/uploads/fastq/single/.keep",
		"organizations/" + orgID + "/uploads/bed/GRCh37/.keep",
		"organizations/" + orgID + "/uploads/bed/GRCh38/.keep",
		"organizations/" + orgID + "/uploads/other/.keep",
		"organizations/" + orgID + "/workflows/.keep",
		"organizations/" + orgID + "/reports/.keep",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected markers:\n%#v", got)
	}
}

func TestTenantUploadObjectKeyRejectsInvalidTenant(t *testing.T) {
	if _, err := tenantUploadObjectKey("../other-tenant", "85bf15f4-77ee-4ddb-a09f-31b00bb29ad8", model.UploadFileTypeOther, "", "file.txt"); err == nil {
		t.Fatal("expected invalid tenant ID to be rejected")
	}
}
