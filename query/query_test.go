package query

import (
	"testing"

	"github.com/oarkflow/vela"
)

func TestQuery(t *testing.T) {
	encoded, _ := vela.Marshal(map[string]any{"profile": map[string]any{"age": uint64(30)}, "status": "paid"})
	v, e := vela.Open(encoded)
	if e != nil {
		t.Fatal(e)
	}
	ok, e := Eq(v, uint64(30), "profile", "age")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
	project, e := Project(v, "status")
	if e != nil || project["status"] != "paid" {
		t.Fatal(project, e)
	}
}
