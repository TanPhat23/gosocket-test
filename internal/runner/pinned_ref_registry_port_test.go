package runner

import "testing"

func TestRequirePinnedRef_RegistryPort(t *testing.T) {
	d := "@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, ok := range []string{"alpine:3.20" + d, "localhost:5000/atm-git-transfer" + d, "reg.example.com:443/a/b:tag" + d} {
		if _, err := requirePinnedRef(ok); err != nil {
			t.Errorf("%q should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"alpine:latest", "localhost:5000/x", "localhost:5000/x:latest", "localhost:/x" + d, "localhost:abc/x" + d, "LOCALHOST:5000/x" + d, "a:123456/x" + d, "alpine@sha256:abc"} {
		if _, err := requirePinnedRef(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}
