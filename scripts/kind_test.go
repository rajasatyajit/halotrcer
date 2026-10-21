//go:build kind

package scripts

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestUmbrellaChartInKind (make helm-kind; integration.yml, never `make
// test`): the umbrella chart, built from the pinned and verified Tokvera and
// Sievant charts exactly as the release builds it (bundled-tools.sh,
// verify-bundled.sh, bundled-chart.sh), installed into a kind cluster with
// both tools switched on. Tokvera's CronJob scans the simulator from the
// pinned Tokvera image (tokvera sim serve), run once now; Sievant's gateway
// Deployment must roll out.
//
// It needs both tools' releases pinned in packaging/bundled-tools.env: their
// charts and images exist nowhere else, and stand-ins (K1) have no charts.
// Until then it skips, saying so. Needs docker, kubectl, helm, oras, cosign
// and go; kind runs as `go run` at the version both tools' kind tests pin.

const (
	// kind v0.33.0 and the node image its release notes name for it, by
	// digest: the pair Tokvera's and Sievant's kind tests use.
	kindModule  = "sigs.k8s.io/kind@v0.33.0"
	kindNode    = "kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5"
	kindCluster = "halotrcer-kind"
	kindNS      = "halotrcer"
	kindRelease = "h"
)

func TestUmbrellaChartInKind(t *testing.T) {
	if b := root(t, "packaging/bundled-tools.env"); !strings.Contains(string(b), "TOKVERA_VERSION=v") || !strings.Contains(string(b), "SIEVANT_VERSION=v") {
		t.Skip("SKIP packaging/bundled-tools.env pins no Tokvera and Sievant releases yet: their charts and images do not exist (PLAN.md K1)")
	}
	need(t, "docker", "kubectl", "helm", "oras", "cosign", "go")
	work := t.TempDir()
	run := func(dir string, name string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "KUBECONFIG="+filepath.Join(work, "kubeconfig"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	// The chart, as the release builds it.
	bundled := filepath.Join(work, "bundled.env")
	writeFile(t, bundled, run("..", "sh", "scripts/release/bundled-tools.sh"), 0o600)
	run("..", "sh", "scripts/release/verify-bundled.sh", bundled)
	run("..", "sh", "scripts/release/bundled-chart.sh", bundled, "v0.0.0-kind", filepath.Join(work, "chart"))
	chart := filepath.Join(work, "chart", "halotrcer-0.0.0-kind.tgz")
	env := readEnv(t, bundled)

	run("", "go", "run", kindModule, "create", "cluster", "--name", kindCluster, "--image", kindNode,
		"--kubeconfig", filepath.Join(work, "kubeconfig"), "--wait", "180s")
	t.Cleanup(func() {
		cmd := exec.CommandContext(t.Context(), "go", "run", kindModule, "delete", "cluster", "--name", kindCluster)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("kind delete cluster: %v\n%s", err, out)
		}
	})
	apply := func(manifest string) {
		t.Helper()
		f := filepath.Join(work, fmt.Sprintf("m%d.yaml", time.Now().UnixNano()))
		writeFile(t, f, manifest, 0o600)
		run("", "kubectl", "apply", "-f", f)
	}
	apply("apiVersion: v1\nkind: Namespace\nmetadata: {name: " + kindNS + "}\n")

	// The endpoint: Tokvera's own simulator, from the pinned Tokvera image.
	apply(fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata: {name: sim, namespace: %[1]s}
spec:
  selector: {matchLabels: {app: sim}}
  template:
    metadata: {labels: {app: sim}}
    spec:
      automountServiceAccountToken: false
      containers:
      - name: sim
        image: %[2]s
        args: [sim, serve, --addr, ":8089"]
        ports: [{containerPort: 8089}]
        readinessProbe: {httpGet: {path: /v1/models, port: 8089}, periodSeconds: 1}
---
apiVersion: v1
kind: Service
metadata: {name: sim, namespace: %[1]s}
spec:
  selector: {app: sim}
  ports: [{port: 8089, targetPort: 8089}]
`, kindNS, env["TOKVERA_IMAGE"]))
	run("", "kubectl", "-n", kindNS, "rollout", "status", "deployment/sim", "--timeout=180s")

	// The gateway's TLS certificate, for its in-cluster name.
	crt, key := selfSigned(t, kindRelease+"-sievant."+kindNS+".svc")
	writeFile(t, filepath.Join(work, "tls.crt"), crt, 0o600)
	writeFile(t, filepath.Join(work, "tls.key"), key, 0o600)
	run("", "kubectl", "-n", kindNS, "create", "secret", "tls", "halotrcer-kind-tls",
		"--cert", filepath.Join(work, "tls.crt"), "--key", filepath.Join(work, "tls.key"))

	values, err := filepath.Abs("testdata/kind-values.yaml")
	if err != nil {
		t.Fatal(err)
	}
	target := fmt.Sprintf("http://sim.%s.svc.cluster.local:8089/v1", kindNS)
	run("", "helm", "install", kindRelease, chart, "-n", kindNS, "-f", values,
		"--set", "tokvera.scan.target="+target, "--wait", "--timeout", "300s")

	// Sievant: the gateway rolls out.
	run("", "kubectl", "-n", kindNS, "rollout", "status", "deployment/"+kindRelease+"-sievant", "--timeout=180s")

	// Tokvera: one run of its CronJob, now, completes.
	cronjob := kindRelease + "-tokvera"
	run("", "kubectl", "-n", kindNS, "create", "job", "now", "--from=cronjob/"+cronjob)
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		conds := run("", "kubectl", "-n", kindNS, "get", "job", "now", "-o", `jsonpath={range .status.conditions[?(@.status=="True")]}{.type} {end}`)
		if strings.Contains(" "+conds, " Complete ") {
			t.Logf("tokvera's scan job completed; sievant's gateway rolled out")
			return
		}
		if strings.Contains(" "+conds, " Failed ") {
			logs, _ := exec.CommandContext(t.Context(), "kubectl", "--kubeconfig", filepath.Join(work, "kubeconfig"), "-n", kindNS, "logs", "job/now", "--all-containers").CombinedOutput()
			t.Fatalf("tokvera's scan job failed:\n%s", logs)
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("tokvera's scan job did not finish in five minutes")
}

// readEnv reads bundled-tools.sh's KEY=value lines.
func readEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			out[k] = v
		}
	}
	return out
}

// selfSigned is a throwaway ECDSA certificate and key for dnsName, in PEM.
func selfSigned(t *testing.T, dnsName string) (string, string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: dnsName}, DNSNames: []string{dnsName},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}))
}
