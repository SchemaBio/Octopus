package resultengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/SchemaBio/Octopus/internal/pathsafe"
)

type sourceIdentity struct {
	hash string
	info os.FileInfo
}

func (e *Engine) openSource(ctx context.Context, q Request) (*Reader, error) {
	path, err := pathsafe.ResolveExistingWithin(e.Root, q.FilePath)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("parquet source is not a regular file")
	}
	cached, ok := e.identities.Load(path)
	identity, valid := cached.(sourceIdentity)
	if !ok || !valid || !os.SameFile(info, identity.info) || info.Size() != identity.info.Size() || info.ModTime() != identity.info.ModTime() {
		hash := sha256.New()
		buffer := make([]byte, 65536)
		for {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			n, readErr := file.Read(buffer)
			if n > 0 {
				hash.Write(buffer[:n])
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return nil, readErr
			}
		}
		after, err := file.Stat()
		if err != nil {
			return nil, err
		}
		if after.Size() != info.Size() || after.ModTime() != info.ModTime() {
			return nil, fmt.Errorf("parquet source changed while hashing")
		}
		identity = sourceIdentity{hash: hex.EncodeToString(hash.Sum(nil)), info: info}
		e.identities.Store(path, identity)
	}
	if identity.hash != q.ObjectHash {
		return nil, fmt.Errorf("parquet fingerprint mismatch")
	}
	return Open(ctx, e.Root, path)
}
