package service

import (
	"github.com/SchemaBio/Octopus/internal/model"
	"strings"
	"testing"
)

func TestVCFSupplementRequiresExactMemberAndKeepsNoCall(t *testing.T) {
	vcf := "#CHROM\tPOS\tID\tREF\tALT\tQUAL\tFILTER\tINFO\tFORMAT\tchild\tfather\tmother\nchr1\t10\t.\tA\tG\t99\tPASS\t.\tGT:DP:GQ:AD:PS\t0|1:30:40:20,10:7\t./.:30:40:30,0:.\t0/0:30:40:30,0:.\n"
	values, err := readAssessmentVCF(strings.NewReader(vcf), []model.ResultMember{{ID: "child", Role: "proband"}, {ID: "father", Role: "father"}, {ID: "mother", Role: "mother"}})
	if err != nil {
		t.Fatal(err)
	}
	got := values["1:10:A:G"]
	if got["proband"].GQ == nil || *got["proband"].GQ != 40 || got["proband"].PS != "7" || got["father"].GT != "./." {
		t.Fatalf("incorrect source genotype mapping: %+v", got)
	}
	unknown, err := readAssessmentVCF(strings.NewReader(vcf), []model.ResultMember{{ID: "different", Role: "proband"}})
	if err != nil || len(unknown) != 0 {
		t.Fatal("guessed member roles")
	}
}
func TestVCFGenotypeMissingQualityIsNotZeroOrHighQuality(t *testing.T) {
	g := parseAssessmentGenotype("GT:DP:AD", "0/1:30:20,10")
	if g.GQ != nil || g.DP == nil {
		t.Fatal("missing GQ invented")
	}
	g = parseAssessmentGenotype("GT:DP:GQ:AD", "0/1:NaN:.:20,x")
	if g.GQ != nil || g.DP != nil || g.AD != nil {
		t.Fatal("invalid source numeric accepted")
	}
}
func TestVCFSupplementRejectsAmbiguousAlleles(t *testing.T) {
	vcf := "#CHROM\tPOS\tID\tREF\tALT\tQUAL\tFILTER\tINFO\tFORMAT\tp\n1\t1\t.\tA\tG\t.\tPASS\t.\tGT\t0/1\n1\t1\t.\tA\tG\t.\tPASS\t.\tGT\t1/1\n"
	if _, err := readAssessmentVCF(strings.NewReader(vcf), []model.ResultMember{{ID: "p", Role: "proband"}}); err == nil {
		t.Fatal("ambiguous allele accepted")
	}
}
