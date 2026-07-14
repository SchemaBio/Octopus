package service

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/model"
)

func actorCanUseArbitraryLocalPaths(actor model.OverlayActor) bool {
	return actor.Role == string(model.SystemRoleSuperAdmin)
}

func validateActorFileReference(cfg *config.Config, actor model.OverlayActor, field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" || actorCanUseArbitraryLocalPaths(actor) {
		return nil
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "cos://") {
		return fmt.Errorf("%s must reference an uploaded or admin-managed local file", field)
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("%s must be an absolute uploaded or admin-managed local file path", field)
	}

	for _, base := range []string{cfg.Storage.LocalDir, cfg.Task.TemplateDir} {
		if strings.TrimSpace(base) == "" {
			continue
		}
		if ensurePathInsideBase(base, value) == nil {
			return nil
		}
	}
	return fmt.Errorf("%s is outside the allowed upload/template directories", field)
}

func validateActorTaskFileInputs(cfg *config.Config, actor model.OverlayActor, inputs map[string]interface{}) error {
	if actorCanUseArbitraryLocalPaths(actor) {
		return nil
	}
	for key, value := range inputs {
		if !isTaskFileInputKey(key) {
			continue
		}
		if err := validateActorTaskInputValue(cfg, actor, key, value); err != nil {
			return err
		}
	}
	return nil
}

func isTaskFileInputKey(key string) bool {
	return containsAny(key, "fastq", "fq", "bam", "cram", "vcf", "gvcf", "bed", "file", "baseline")
}

func validateActorTaskInputValue(cfg *config.Config, actor model.OverlayActor, key string, value interface{}) error {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return validateActorFileReference(cfg, actor, "input "+key, v)
	case []string:
		for _, item := range v {
			if err := validateActorFileReference(cfg, actor, "input "+key, item); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, item := range v {
			if err := validateActorTaskInputValue(cfg, actor, key, item); err != nil {
				return err
			}
		}
	}
	return nil
}
