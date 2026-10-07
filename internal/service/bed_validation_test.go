package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBEDContentValidation(t *testing.T) {
	index := map[string]int64{"1": 100, "MT": 20}
	for _, c := range []struct {
		name, content string
		valid         bool
	}{
		{"valid", "# panel\ntrack name=test\nchr1\t0\t100\nchrM\t0\t20\n", true},
		{"missing interval", "# comment\n", false}, {"negative", "1\t-1\t10\n", false}, {"unknown contig", "2\t0\t1\n", false}, {"out of bounds", "1\t0\t101\n", false}, {"bad coordinate", "1\tx\t10\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := validateBEDStream(context.Background(), strings.NewReader(c.content), false, index)
			if (e == nil) != c.valid {
				t.Fatalf("valid=%v error=%v", c.valid, e)
			}
			if !c.valid && !errors.Is(e, errBEDContent) {
				t.Fatalf("invalid content misclassified: %v", e)
			}
		})
	}
	var compressed bytes.Buffer
	g := gzip.NewWriter(&compressed)
	g.Write([]byte("1\t0\t10\n"))
	g.Close()
	if e := validateBEDStream(context.Background(), bytes.NewReader(compressed.Bytes()), true, index); e != nil {
		t.Fatal(e)
	}
	damaged := append([]byte(nil), compressed.Bytes()...)
	damaged[len(damaged)-8] ^= 0xff
	if e := validateBEDStream(context.Background(), bytes.NewReader(damaged), true, index); !errors.Is(e, errBEDContent) {
		t.Fatalf("CRC damage should be invalid content: %v", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := validateBEDStream(ctx, strings.NewReader("1\t0\t1\n"), false, index); !errors.Is(e, context.Canceled) {
		t.Fatalf("infrastructure cancellation misclassified: %v", e)
	}
}
func TestBEDIndexAliasAmbiguity(t *testing.T) {
	if _, e := parseBEDIndex(strings.NewReader("1\t100\nchr1\t101\n")); e == nil {
		t.Fatal("ambiguous aliases accepted")
	}
}
