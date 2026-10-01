package service

import (
	"context"
	"github.com/SchemaBio/Octopus/internal/model"
	"path"
	"strings"
)

func (s *ResultService) InspectParquetArchive(ctx context.Context, task *model.Task) (map[string]interface{}, error) {
	storage, err := newS3Storage(ctx, s.cfg.Storage)
	if err != nil {
		return nil, err
	}
	prefix := resultPackagePrefix(task)
	objects, err := storage.list(ctx, prefix+"/")
	if err != nil {
		return nil, err
	}
	key, _, version, err := igvArchiveObjectIndex(prefix, objects)
	if err != nil {
		return nil, err
	}
	manifest, err := readIGVManifest(ctx, storage, key)
	if err != nil {
		return nil, err
	}
	files := []map[string]interface{}{}
	for _, object := range objects {
		if strings.HasSuffix(object.Key, ".parquet") {
			files = append(files, map[string]interface{}{"name": path.Base(object.Key), "size": object.Size})
		}
	}
	refs := []map[string]interface{}{}
	for _, ref := range manifestParquetRefs(manifest) {
		refs = append(refs, map[string]interface{}{"name": path.Base(ref), "full_prefix": strings.HasPrefix(ref, prefix+"/"), "absolute_path": strings.HasPrefix(ref, "/"), "scheme": strings.Contains(ref, "://")})
	}
	return map[string]interface{}{"manifest": path.Base(key), "manifest_version": version, "parquet_objects": files, "parquet_references": refs}, nil
}
