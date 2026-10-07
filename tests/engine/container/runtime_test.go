package container_test

import (
	"os/exec"
	"testing"
)

func TestAddressableNetworkUsesAllocatedSubnet(t *testing.T) {
	// Validate command order without requiring a running Docker daemon. An
	// allocation failure must never remove an existing network.
	for _, fail := range []string{"false", "true"} {
		script := `set -euo pipefail
source ./runtime.sh
state=0
docker() {
  if [[ "$*" == "network create metis-test-owned" && "$state" == 0 ]]; then
    if ` + fail + `; then return 1; fi
    state=1
  elif [[ "$1 $2 $3" == "network inspect metis-test-owned" && "$state" == 1 ]]; then
    printf '172.31.0.0/16\n'
  elif [[ "$*" == "network rm metis-test-owned" && "$state" == 1 ]]; then
    state=2
  elif [[ "$*" == "network create --subnet 172.31.0.0/16 metis-test-owned" && "$state" == 2 ]]; then
    state=3
  else
    printf 'unexpected Docker operation: %s\n' "$*" >&2
    return 99
  fi
}
if metis_container_create_addressable_network metis-test-owned; then
  [[ "$state" == 3 && ` + fail + ` == false ]]
else
  [[ "$state" == 0 && ` + fail + ` == true ]]
fi`
		if output, err := exec.Command("bash", "-c", script).CombinedOutput(); err != nil {
			t.Fatalf("fail=%s: %s %v", fail, output, err)
		}
	}
}
