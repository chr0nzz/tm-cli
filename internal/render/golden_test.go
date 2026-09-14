package render

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/chr0nzz/tm-cli/internal/answers"
	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "rewrite golden files")

const goldenRoot = "../../testdata/golden"

const testUser = "alice"

func TestGolden(t *testing.T) {
	entries, err := os.ReadDir(goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		count++
		t.Run(e.Name(), func(t *testing.T) {
			runScenario(t, filepath.Join(goldenRoot, e.Name()))
		})
	}
	if count == 0 {
		t.Fatal("no golden scenarios found")
	}
}

func loadScenario(t *testing.T, dir string) *answers.Answers {
	t.Helper()
	a, err := answers.Load(filepath.Join(dir, "answers.yml"))
	if err != nil {
		t.Fatal(err)
	}
	a.Finalize()
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, k := range a.SecretKeys() {
		if a.Secrets[k] == "" {
			t.Fatalf("answers.yml must provide secret %s", k)
		}
	}
	return a
}

const staticFixture = "input/traefik.yml"

func loadStatic(t *testing.T, dir string, a *answers.Answers) Static {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, staticFixture))
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if a.CrowdSec.BouncerPlugin && !a.Mode.HasTraefik() {
			t.Fatalf("%s needs %s: the bouncer plugin is declared in the traefik.yml this mode does not generate", dir, staticFixture)
		}
		return Static{}
	}
	st := StaticFrom(a.Mounts.StaticConfigPath, data)
	if !st.Ready {
		t.Fatalf("%s: %s", staticFixture, st.Note)
	}
	return st
}

func runScenario(t *testing.T, dir string) {
	a := loadScenario(t, dir)
	static := loadStatic(t, dir, a)
	out, err := Render(Input{Answers: a, User: testUser, Static: static})
	if err != nil {
		t.Fatal(err)
	}
	checkOutput(t, a, static, out)
	if *update {
		writeGolden(t, dir, out)
		return
	}
	compareGolden(t, dir, out)
}

func goldenFilePath(p string) string {
	return filepath.Join("files", strings.TrimPrefix(p, "/"))
}

func manifestLine(f File) string {
	line := fmt.Sprintf("%s %04o", f.Path, f.Mode)
	if f.CreateOnly {
		line += " create-only"
	}
	if f.Fill {
		line += " fill"
	}
	if f.Untracked {
		line += " untracked"
	}
	if f.Privileged {
		line += " privileged"
	}
	return line
}

func writeGolden(t *testing.T, dir string, out *Output) {
	t.Helper()
	files := filepath.Join(dir, "files")
	if err := os.RemoveAll(files); err != nil {
		t.Fatal(err)
	}
	var dirs, manifest []string
	dirs = append(dirs, out.Dirs...)
	for _, f := range out.Files {
		manifest = append(manifest, manifestLine(f))
		p := filepath.Join(dir, goldenFilePath(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "dirs.txt"), []byte(joinLines(dirs)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "files.txt"), []byte(joinLines(manifest)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func compareGolden(t *testing.T, dir string, out *Output) {
	t.Helper()
	wantDirs, err := os.ReadFile(filepath.Join(dir, "dirs.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := joinLines(out.Dirs); got != string(wantDirs) {
		t.Errorf("dirs mismatch\n got: %q\nwant: %q", got, string(wantDirs))
	}
	wantManifest, err := os.ReadFile(filepath.Join(dir, "files.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest []string
	for _, f := range out.Files {
		manifest = append(manifest, manifestLine(f))
	}
	if got := joinLines(manifest); got != string(wantManifest) {
		t.Errorf("file manifest mismatch\n got:\n%s\nwant:\n%s", got, string(wantManifest))
	}
	for _, f := range out.Files {
		want, err := os.ReadFile(filepath.Join(dir, goldenFilePath(f.Path)))
		if err != nil {
			t.Errorf("%s: %v", f.Path, err)
			continue
		}
		if f.Content != string(want) {
			t.Errorf("%s differs from golden\n--- got ---\n%s\n--- want ---\n%s", f.Path, f.Content, string(want))
		}
	}
}

func checkOutput(t *testing.T, a *answers.Answers, static Static, out *Output) {
	t.Helper()
	plan := Bouncer(a, static)
	inUsersTraefik := map[string]bool{}
	if plan.Enabled {
		inUsersTraefik[plan.StaticPath] = true
		inUsersTraefik[plan.MiddlewarePath] = true
	}
	seen := map[string]bool{}
	for _, f := range out.Files {
		if seen[f.Path] {
			t.Errorf("duplicate file %s", f.Path)
		}
		seen[f.Path] = true
		if f.Path == "" || f.Mode == 0 {
			t.Errorf("file %q has no path or mode", f.Path)
		}
		abs := strings.HasPrefix(f.Path, "/")
		if abs != a.Mode.IsSystemd() && !inUsersTraefik[f.Path] {
			t.Errorf("%s: absolute paths are for systemd modes only, or the bouncer files in the user's own Traefik tree", f.Path)
		}
		if abs != f.Privileged {
			t.Errorf("%s: privileged must be set exactly for absolute paths", f.Path)
		}
		if f.CreateOnly && f.Fill {
			t.Errorf("%s: create-only and fill are alternatives, not both", f.Path)
		}
		if f.Content != "" && !strings.HasSuffix(f.Content, "\n") {
			t.Errorf("%s does not end with a newline", f.Path)
		}
		if strings.Contains(f.Content, "PLACEHOLDER") && !isSecretFile(f.Path) && f.Path != plan.MiddlewarePath {
			t.Errorf("%s leaks a secret value", f.Path)
		}
		ext := filepath.Ext(f.Path)
		if ext == ".yml" || ext == ".yaml" {
			var doc any
			if err := yaml.Unmarshal([]byte(f.Content), &doc); err != nil {
				t.Errorf("%s is not valid yaml: %v\n%s", f.Path, err, f.Content)
				continue
			}
			if filepath.Base(f.Path) == "docker-compose.yml" {
				checkCompose(t, f.Path, doc)
			}
		}
	}
	if plan.On && plan.Enabled {
		checkBouncer(t, a, plan, out)
	}
	for _, d := range out.Dirs {
		if d == "" {
			t.Error("empty dir entry")
		}
		if strings.HasPrefix(d, "/") != a.Mode.IsSystemd() {
			t.Errorf("dir %s: absolute paths are for systemd modes only", d)
		}
	}
	for _, k := range a.SecretKeys() {
		if !referencesSecret(out, k) {
			t.Errorf("secret %s is never referenced by the rendered files", k)
		}
	}
}

func checkBouncer(t *testing.T, a *answers.Answers, plan BouncerPlan, out *Output) {
	t.Helper()
	static, ok := findFile(out, plan.StaticPath)
	if !ok {
		t.Fatalf("the bouncer plugin is on but nothing writes %s", plan.StaticPath)
	}
	declared := "\n    " + plan.Alias + ":\n      moduleName: " + BouncerModule + "\n      version: " + BouncerVersion + "\n"
	if !strings.Contains(static.Content, declared) {
		t.Errorf("%s does not declare the bouncer under the alias %q:\n%s", plan.StaticPath, plan.Alias, static.Content)
	}
	if plan.MiddlewarePath == "" {
		if plan.Note == "" {
			t.Error("no middleware was written and no reason was given")
		}
		return
	}
	mw, ok := findFile(out, plan.MiddlewarePath)
	if !ok {
		t.Fatalf("no middleware at %s", plan.MiddlewarePath)
	}
	if !strings.Contains(mw.Content, "\n        "+plan.Alias+":\n") {
		t.Errorf("%s does not key the plugin block on the declared alias %q:\n%s", plan.MiddlewarePath, plan.Alias, mw.Content)
	}
	if !strings.Contains(mw.Content, "CrowdsecLapiHost: "+plan.LapiHost+"\n") {
		t.Errorf("%s does not point at the lapi host %q:\n%s", plan.MiddlewarePath, plan.LapiHost, mw.Content)
	}
	if key := a.Secrets[answers.SecretCrowdSecAPIKey]; key != "" && !strings.Contains(mw.Content, "CrowdsecLapiKey: "+key+"\n") {
		t.Errorf("%s does not carry the bouncer key:\n%s", plan.MiddlewarePath, mw.Content)
	}
	for _, ip := range plan.TrustedIPs {
		if !strings.Contains(mw.Content, "\n            - "+ip+"\n") {
			t.Errorf("%s does not seed the trusted ip %s:\n%s", plan.MiddlewarePath, ip, mw.Content)
		}
	}
}

func isSecretFile(p string) bool {
	return filepath.Base(p) == ".env" || p == AgentEnvPath || p == NativeEnvPath
}

func isEnvFileUnit(p string) bool {
	switch filepath.Base(p) {
	case "tma.service", "traefik-manager.service", "traefik.service":
		return true
	}
	return false
}

func referencesSecret(out *Output, key string) bool {
	for _, f := range out.Files {
		if isSecretFile(f.Path) {
			continue
		}
		if strings.Contains(f.Content, "${"+key+"}") {
			return true
		}
		if isEnvFileUnit(f.Path) && strings.Contains(f.Content, "EnvironmentFile=") {
			return true
		}
	}
	return false
}

func checkCompose(t *testing.T, path string, doc any) {
	t.Helper()
	root, ok := doc.(map[string]any)
	if !ok {
		t.Errorf("%s: compose root is not a mapping", path)
		return
	}
	services, ok := root["services"].(map[string]any)
	if !ok || len(services) == 0 {
		t.Errorf("%s: compose has no services", path)
		return
	}
	networks, _ := root["networks"].(map[string]any)
	volumes, _ := root["volumes"].(map[string]any)
	for _, k := range []string{"networks", "volumes", "services"} {
		if v, present := root[k]; present && v == nil {
			t.Errorf("%s: top-level %s is empty", path, k)
		}
	}
	names := sortedKeys(services)
	for _, name := range names {
		svc, ok := services[name].(map[string]any)
		if !ok {
			t.Errorf("%s: service %s is not a mapping", path, name)
			continue
		}
		if img, _ := svc["image"].(string); img == "" {
			t.Errorf("%s: service %s has no image", path, name)
		}
		for _, n := range stringList(svc["networks"]) {
			if _, ok := networks[n]; !ok {
				t.Errorf("%s: service %s uses undefined network %s", path, name, n)
			}
		}
		for _, v := range stringList(svc["volumes"]) {
			src := strings.SplitN(v, ":", 2)[0]
			if strings.HasPrefix(src, "/") || strings.HasPrefix(src, "./") {
				continue
			}
			if _, ok := volumes[src]; !ok {
				t.Errorf("%s: service %s uses undefined volume %s", path, name, src)
			}
		}
		for _, d := range stringList(svc["depends_on"]) {
			if _, ok := services[d]; !ok {
				t.Errorf("%s: service %s depends on unknown service %s", path, name, d)
			}
		}
		for _, e := range stringList(svc["environment"]) {
			if !strings.Contains(e, "=") {
				t.Errorf("%s: service %s has malformed environment entry %q", path, name, e)
			}
		}
		for _, key := range []string{"ports", "volumes", "labels", "depends_on"} {
			if v, present := svc[key]; present && v == nil {
				t.Errorf("%s: service %s has an empty %s block", path, name, key)
			}
		}
	}
}

func stringList(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
