package main

// Stable IDs for YiJian frontend local dev. Documented in docs/dev-frontend.md.
const (
	seedMarker = "[seed]"

	// Tasks (public UUID used by API and result tables)
	taskCompletedUUID = "11111111-1111-4111-8111-111111111101"
	taskRunningUUID   = "11111111-1111-4111-8111-111111111102"
	taskQueuedUUID    = "11111111-1111-4111-8111-111111111103"
	taskFailedUUID    = "11111111-1111-4111-8111-111111111104"

	// Samples
	sampleProbandUUID = "22222222-2222-4222-8222-222222222201"
	sampleFatherUUID  = "22222222-2222-4222-8222-222222222202"
	sampleMotherUUID  = "22222222-2222-4222-8222-222222222203"
	sampleOtherUUID   = "22222222-2222-4222-8222-222222222204"

	// Pedigree
	pedigreeUUID       = "33333333-3333-4333-8333-333333333301"
	memberProbandUUID  = "33333333-3333-4333-8333-333333333311"
	memberFatherUUID   = "33333333-3333-4333-8333-333333333312"
	memberMotherUUID   = "33333333-3333-4333-8333-333333333313"
	memberSiblingUUID  = "33333333-3333-4333-8333-333333333314"
	memberPGFUUID      = "33333333-3333-4333-8333-333333333315"
	memberPGMUUID      = "33333333-3333-4333-8333-333333333316"

	// Pipelines / gene lists
	pipelineWESUUID   = "44444444-4444-4444-8444-444444444401"
	pipelinePanelUUID = "44444444-4444-4444-8444-444444444402"
	geneListCoreUUID  = "55555555-5555-4555-8555-555555555501"
	geneListImpUUID   = "55555555-5555-4555-8555-555555555502"

	// QC
	qcResultID = "66666666-6666-4666-8666-666666666601"
)

var seedSampleUUIDs = []string{
	sampleProbandUUID,
	sampleFatherUUID,
	sampleMotherUUID,
	sampleOtherUUID,
}

var seedTaskUUIDs = []string{
	taskCompletedUUID,
	taskRunningUUID,
	taskQueuedUUID,
	taskFailedUUID,
}

var seedPipelineIDs = []string{pipelineWESUUID, pipelinePanelUUID}
var seedGeneListIDs = []string{geneListCoreUUID, geneListImpUUID}
var seedPedigreeIDs = []string{pedigreeUUID}
var seedMemberIDs = []string{
	memberProbandUUID,
	memberFatherUUID,
	memberMotherUUID,
	memberSiblingUUID,
	memberPGFUUID,
	memberPGMUUID,
}
