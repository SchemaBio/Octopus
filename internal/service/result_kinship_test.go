package service

import (
	"strings"
	"testing"
)

const kinshipHeader = "sample_a,sample_b,rel,pedigree_relatedness,ibs0,ibs2,n,pedigree_parents,predicted_parents,parent_error,sample_duplication_error\n"

func TestKinshipReport(t *testing.T) {
	raw := kinshipHeader + "father,mother,-0.06313,0,545,2133,4920,False,False,False,False\nfather,proband,0.6492,0.5,6,2789,4920,True,True,False,False\n"
	pairs, err := parseKinshipReport(raw)
	if err != nil || len(pairs) != 2 {
		t.Fatalf("parse: %v", err)
	}
	if pairs[0].Relatedness != -0.06313 || pairs[0].PedigreeRelatedness != 0 || pairs[0].PredictedParents || !pairs[1].PredictedParents || pairs[1].ParentError {
		t.Fatal("source flags or zero/negative metrics lost")
	}
	for name, bad := range map[string]string{
		"nan": strings.Replace(raw, "-0.06313", "NaN", 1), "inf": strings.Replace(raw, "-0.06313", "+Inf", 1), "flag": strings.Replace(raw, "False", "unknown", 1), "sites": strings.Replace(raw, "4920", "0", 1), "count": strings.Replace(raw, "545", "99999", 1), "header": "sample_a,sample_b\na,b\n", "duplicate": raw + "mother,father,0,0,0,0,1,False,False,False,False\n", "truncated": raw + "father,proband,0\n"} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseKinshipReport(bad); err == nil {
				t.Fatal("accepted invalid report")
			}
		})
	}
	empty, err := parseKinshipReport(kinshipHeader)
	if err != nil || len(empty) != 0 {
		t.Fatal("empty report not distinguished")
	}
}
func TestKinshipLocalManifestRefs(t *testing.T) {
	refs, err := kinshipLocalRefs([]byte(`{"summary":{"peddy":"outputs/family.ped_check.csv","other":"some.csv"}}`))
	if err != nil || len(refs) != 1 || refs[0] != "outputs/family.ped_check.csv" {
		t.Fatal(refs, err)
	}
	if _, err := kinshipLocalRefs([]byte(`not json`)); err == nil {
		t.Fatal("accepted malformed manifest")
	}
}
