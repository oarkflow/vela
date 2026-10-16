package batch

import (
	"github.com/oarkflow/vela"
)

// WriteFile persists a self-contained, CRC32C-protected batch in VELA's
// atomic file envelope. The additional bytes envelope is intentional: batch
// pages themselves remain VLB1 and can be transported directly in memory.
func WriteFile(path string, rows []map[string]any) error {
	data, err := MarshalFast(rows)
	if err != nil {
		return err
	}
	return vela.MarshalFile(path, data)
}

// ReadFile validates the envelope and the entire batch before returning.
func ReadFile(path string) (*Batch, error) {
	v, err := vela.UnmarshalFile(path)
	if err != nil {
		return nil, err
	}
	raw, ok := v.([]byte)
	if !ok {
		return nil, ErrBatch
	}
	return Open(raw)
}
