// Command seed loads demo data for YiJian frontend local development.
//
//	go run ./cmd/seed
//	go run ./cmd/seed -reset
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/service"
	"gorm.io/gorm"
)

func main() {
	reset := flag.Bool("reset", false, "delete existing seed data before inserting")
	flag.Parse()

	cfg := config.Load()
	if err := database.InitDB(cfg); err != nil {
		fatalf("database: %v", err)
	}
	defer database.CloseDB()

	if err := database.AutoMigrate(); err != nil {
		fatalf("migrate: %v", err)
	}

	db := database.GetDB()
	if *reset {
		fmt.Println("Resetting seed data...")
		if err := deleteSeedData(db); err != nil {
			fatalf("reset: %v", err)
		}
	} else if seedAlreadyPresent(db) {
		fmt.Println("Seed data already present (use -reset to reinsert).")
		return
	}

	adminEmail := envOr("DEFAULT_ADMIN_EMAIL", "admin@octopus.local")
	adminPassword := envOr("DEFAULT_ADMIN_PASSWORD", "admin123")

	userSvc := service.NewUserService(cfg)
	admin, err := userSvc.CreateDefaultAdmin(adminEmail, adminPassword, "Administrator")
	if err != nil {
		fatalf("admin: %v", err)
	}
	fmt.Printf("Admin ready: %s (id=%d)\n", admin.Email, admin.ID)

	if err := insertSeedData(db, admin.ID); err != nil {
		fatalf("seed: %v", err)
	}

	fmt.Println("Seed complete.")
	fmt.Printf("  Login: %s / %s\n", adminEmail, adminPassword)
	fmt.Printf("  Completed task: %s\n", taskCompletedUUID)
	fmt.Printf("  YiJian API base: http://localhost:8080/api\n")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func seedAlreadyPresent(db *gorm.DB) bool {
	var n int64
	db.Model(&model.Task{}).Where("uuid = ?", taskCompletedUUID).Count(&n)
	return n > 0
}

func deleteSeedData(db *gorm.DB) error {
	// Results first (by task uuid)
	for _, tid := range seedTaskUUIDs {
		if err := db.Where("task_id = ?", tid).Delete(&model.SNVIndel{}).Error; err != nil {
			return err
		}
		if err := db.Where("task_id = ?", tid).Delete(&model.CNVSegment{}).Error; err != nil {
			return err
		}
		if err := db.Where("task_id = ?", tid).Delete(&model.CNVExon{}).Error; err != nil {
			return err
		}
		if err := db.Where("task_id = ?", tid).Delete(&model.QCResult{}).Error; err != nil {
			return err
		}
		if err := db.Where("task_id = ?", tid).Delete(&model.CNVAssessment{}).Error; err != nil {
			return err
		}
	}

	if err := db.Where("uuid IN ?", seedTaskUUIDs).Delete(&model.Task{}).Error; err != nil {
		return err
	}
	if err := db.Where("id IN ?", seedMemberIDs).Delete(&model.PedigreeMember{}).Error; err != nil {
		return err
	}
	if err := db.Where("id IN ?", seedPedigreeIDs).Delete(&model.Pedigree{}).Error; err != nil {
		return err
	}
	if err := db.Where("uuid IN ?", seedSampleUUIDs).Delete(&model.Sample{}).Error; err != nil {
		return err
	}
	if err := db.Where("id IN ?", seedPipelineIDs).Delete(&model.Pipeline{}).Error; err != nil {
		return err
	}
	if err := db.Where("id IN ?", seedGeneListIDs).Delete(&model.GeneList{}).Error; err != nil {
		return err
	}
	return nil
}

func insertSeedData(db *gorm.DB, adminID uint) error {
	now := time.Now()

	samples := buildSamples(adminID, now)
	for i := range samples {
		if err := db.Create(&samples[i]).Error; err != nil {
			return fmt.Errorf("sample %s: %w", samples[i].UUID, err)
		}
	}

	pipelines := buildPipelines(adminID, now)
	for i := range pipelines {
		if err := db.Create(&pipelines[i]).Error; err != nil {
			return fmt.Errorf("pipeline %s: %w", pipelines[i].ID, err)
		}
	}

	geneLists := buildGeneLists(adminID, now)
	for i := range geneLists {
		if err := db.Create(&geneLists[i]).Error; err != nil {
			return fmt.Errorf("gene list %s: %w", geneLists[i].ID, err)
		}
	}

	pedigree, members := buildPedigree(adminID, now)
	if err := db.Create(&pedigree).Error; err != nil {
		return fmt.Errorf("pedigree: %w", err)
	}
	for i := range members {
		if err := db.Create(&members[i]).Error; err != nil {
			return fmt.Errorf("member %s: %w", members[i].ID, err)
		}
	}

	tasks := buildTasks(adminID, now)
	for i := range tasks {
		if err := db.Create(&tasks[i]).Error; err != nil {
			return fmt.Errorf("task %s: %w", tasks[i].UUID, err)
		}
	}

	qc := buildQC(now)
	if err := db.Create(&qc).Error; err != nil {
		return fmt.Errorf("qc: %w", err)
	}

	snvs := buildSNVs(now)
	if err := db.CreateInBatches(snvs, 50).Error; err != nil {
		return fmt.Errorf("snv: %w", err)
	}

	segments := buildCNVSegments(now)
	if err := db.CreateInBatches(segments, 50).Error; err != nil {
		return fmt.Errorf("cnv segment: %w", err)
	}

	exons := buildCNVExons(now)
	if err := db.CreateInBatches(exons, 50).Error; err != nil {
		return fmt.Errorf("cnv exon: %w", err)
	}

	fmt.Printf("  samples=%d pipelines=%d gene_lists=%d pedigree_members=%d tasks=%d snv=%d cnv_seg=%d cnv_exon=%d\n",
		len(samples), len(pipelines), len(geneLists), len(members), len(tasks), len(snvs), len(segments), len(exons))
	return nil
}
