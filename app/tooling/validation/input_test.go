package validation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const example = `{"schema_version":1,"queries":[{"id":"revenue","query":{"project":"sales","model":"sales","metrics":[{"name":"total_revenue"}],"filters":{"kind":"filter","filter":{"field":"region","operator":"eq","value":"APAC"}}}}]}`

func TestInventoryStrictness(t *testing.T) {
	if _, err := ParseInventory([]byte(example), "sales"); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{
		strings.Replace(example, `"name":"total_revenue"`, `"name":"total_revenue","sql":"secret"`, 1),
		strings.Replace(example, `"operator":"eq"`, `"operator":"eq","unknown":true`, 1),
		strings.Replace(example, `"project":"sales"`, `"project":"sales","project":"sales"`, 1),
		strings.Replace(example, `"sales"`, `"other"`, 1),
		example + example,
		`{"schema_version":1,"queries":[]}`,
	} {
		if _, err := ParseInventory([]byte(data), "sales"); err == nil {
			t.Fatalf("accepted invalid inventory: %s", data)
		}
	}
	for _, literal := range []string{"9007199254740993", "0.10000000000000000001"} {
		input := strings.Replace(example, `"value":"APAC"`, `"value":`+literal, 1)
		inventory, err := ParseInventory([]byte(input), "sales")
		if err != nil {
			t.Fatalf("exact numeric filter %s: %v", literal, err)
		}
		if got := inventory.Queries[0].Query.Filters.Leaves()[0].Value; fmt.Sprint(got) != literal {
			t.Fatalf("numeric filter = %#v, want %s", got, literal)
		}
	}
}

func TestReportPublicationIsPrivateAndExclusive(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "model.yaml")
	if err := os.WriteFile(input, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "report.json")
	if err := WriteReport(output, Report{Mode: "online"}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(output)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
	for _, path := range []string{input, output} {
		if err := WriteReport(path, Report{}); err == nil {
			t.Fatal("overwrote existing file")
		}
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(input, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteReport(link, Report{}); err == nil {
		t.Fatal("overwrote symlink")
	}
	data, _ := os.ReadFile(input)
	if string(data) != "original" {
		t.Fatal("changed input")
	}
}
