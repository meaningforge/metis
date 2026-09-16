package bootstrap

import (
	"reflect"
	"testing"
)

func TestLoadDeploymentConfigAcceptsMultipleProjectDataSources(t *testing.T) {
	config, err := loadDeploymentConfig([]byte(`
projects:
  analytics:
    path: project.yaml
    data_sources: [sales-db, customer-db]
data_sources:
  path: datasources.yaml
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := config.DataSourcesForProject("analytics"); !reflect.DeepEqual(got, []string{"sales-db", "customer-db"}) {
		t.Fatalf("DataSourcesForProject() = %#v", got)
	}
	if _, ok := config.DataSourceForProject("analytics"); ok {
		t.Fatal("multi-source project exposed an ambiguous singular DataSource")
	}
}

func TestLoadDeploymentConfigRejectsInvalidProjectDataSourceSets(t *testing.T) {
	for _, test := range []struct {
		name string
		yaml string
	}{
		{
			name: "singular and plural",
			yaml: "projects:\n  p:\n    path: p.yaml\n    data_source: one\n    data_sources: [two]\ndata_sources:\n  path: datasources.yaml\n",
		},
		{
			name: "duplicate",
			yaml: "projects:\n  p:\n    path: p.yaml\n    data_sources: [one, one]\ndata_sources:\n  path: datasources.yaml\n",
		},
		{
			name: "blank",
			yaml: "projects:\n  p:\n    path: p.yaml\n    data_sources: [one, '']\ndata_sources:\n  path: datasources.yaml\n",
		},
		{
			name: "missing registry",
			yaml: "projects:\n  p:\n    path: p.yaml\n    data_sources: [one]\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := loadDeploymentConfig([]byte(test.yaml)); err == nil {
				t.Fatal("invalid Project DataSource set accepted")
			}
		})
	}
}
