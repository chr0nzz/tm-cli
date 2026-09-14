package render

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/chr0nzz/tm-cli/internal/answers"
)

func TestParseStaticSeedsTrustedIPs(t *testing.T) {
	cases := []struct {
		name   string
		static string
		want   []string
	}{
		{
			name: "the shape a real install has, trusted ips on the https entry point only",
			static: `entryPoints:
  http:
    address: ":80"
  https:
    address: ":443"
    forwardedHeaders:
      trustedIPs:
        - 127.0.0.1/32
        - 172.17.0.0/16
        - 100.64.0.0/10
`,
			want: []string{"127.0.0.1/32", "172.17.0.0/16", "100.64.0.0/10"},
		},
		{
			name: "two entry points, order preserved and duplicates dropped",
			static: `entryPoints:
  web:
    forwardedHeaders:
      trustedIPs:
        - 10.0.0.0/8
        - 127.0.0.1/32
  websecure:
    forwardedHeaders:
      trustedIPs:
        - 127.0.0.1/32
        - 172.18.0.0/16
`,
			want: []string{"10.0.0.0/8", "127.0.0.1/32", "172.18.0.0/16"},
		},
		{
			name: "flow sequence",
			static: `entryPoints:
  websecure: {forwardedHeaders: {trustedIPs: [127.0.0.1/32, 100.64.0.0/10]}}
`,
			want: []string{"127.0.0.1/32", "100.64.0.0/10"},
		},
		{
			name: "traefik reads element names without regard to case",
			static: `entrypoints:
  websecure:
    forwardedheaders:
      trustedips:
        - 192.168.1.0/24
`,
			want: []string{"192.168.1.0/24"},
		},
		{
			name: "insecure is a bool, not a trusted ip list",
			static: `entryPoints:
  websecure:
    forwardedHeaders:
      insecure: true
`,
			want: nil,
		},
		{
			name:   "nothing to seed",
			static: "entryPoints:\n  web:\n    address: \":80\"\n",
			want:   nil,
		},
		{
			name:   "no entry points at all",
			static: "providers:\n  file:\n    directory: /etc/traefik/conf.d\n",
			want:   nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseStatic([]byte(c.static))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.TrustedIPs, c.want) {
				t.Fatalf("trusted ips = %q, want %q", got.TrustedIPs, c.want)
			}
		})
	}
}

func TestParseStaticFindsTheBouncerUnderAnyAlias(t *testing.T) {
	static := `experimental:
  plugins:
    rewrite:
      moduleName: github.com/traefik/plugin-rewritebody
      version: v0.3.1
    bouncer:
      moduleName: ` + BouncerModule + `
      version: v1.6.0
`
	got, err := ParseStatic([]byte(static))
	if err != nil {
		t.Fatal(err)
	}
	if got.Alias != "bouncer" {
		t.Fatalf("alias = %q, want bouncer", got.Alias)
	}
}

func TestParseStaticRejectsANonMapping(t *testing.T) {
	if _, err := ParseStatic([]byte("- a\n- b\n")); err == nil {
		t.Fatal("a sequence is not a traefik static config")
	}
	if _, err := ParseStatic([]byte("a: [\n")); err == nil {
		t.Fatal("broken yaml must not parse")
	}
}

func TestMergeStatic(t *testing.T) {
	cases := []struct {
		name   string
		static string
		want   string
	}{
		{
			name:   "no experimental block, appended at the end",
			static: "api:\n  dashboard: true\n",
			want: `api:
  dashboard: true

experimental:
  plugins:
    crowdsec:
      moduleName: ` + BouncerModule + `
      version: ` + BouncerVersion + `
`,
		},
		{
			name:   "an empty file",
			static: "",
			want: `experimental:
  plugins:
    crowdsec:
      moduleName: ` + BouncerModule + `
      version: ` + BouncerVersion + `
`,
		},
		{
			name:   "an existing experimental block without plugins",
			static: "experimental:\n  localPlugins:\n    x:\n      moduleName: local\n\nlog:\n  level: INFO\n",
			want: `experimental:
  plugins:
    crowdsec:
      moduleName: ` + BouncerModule + `
      version: ` + BouncerVersion + `
  localPlugins:
    x:
      moduleName: local

log:
  level: INFO
`,
		},
		{
			name:   "an existing plugins block keeps its neighbours",
			static: "experimental:\n  plugins:\n    rewrite:\n      moduleName: github.com/traefik/plugin-rewritebody\n",
			want: `experimental:
  plugins:
    crowdsec:
      moduleName: ` + BouncerModule + `
      version: ` + BouncerVersion + `
    rewrite:
      moduleName: github.com/traefik/plugin-rewritebody
`,
		},
		{
			name:   "four space indentation is followed",
			static: "experimental:\n    plugins:\n        rewrite:\n            moduleName: github.com/traefik/plugin-rewritebody\n",
			want: `experimental:
    plugins:
        crowdsec:
            moduleName: ` + BouncerModule + `
            version: ` + BouncerVersion + `
        rewrite:
            moduleName: github.com/traefik/plugin-rewritebody
`,
		},
		{
			name:   "comments and blank lines survive",
			static: "# my traefik\napi:\n  dashboard: true  # on purpose\n\n# plugins below\nexperimental:\n  # none yet\n  plugins: {}\n",
			want:   "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := MergeStatic([]byte(c.static), BouncerAlias)
			if c.want == "" {
				if err == nil {
					t.Fatalf("an inline plugins mapping cannot be spliced, got:\n%s", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("merged:\n%s\nwant:\n%s", got, c.want)
			}
			var doc any
			if err := yaml.Unmarshal([]byte(got), &doc); err != nil {
				t.Fatalf("the merged file is not valid yaml: %v", err)
			}
			facts, err := ParseStatic([]byte(got))
			if err != nil {
				t.Fatal(err)
			}
			if facts.Alias != BouncerAlias {
				t.Fatalf("the merged file does not declare the bouncer, alias = %q", facts.Alias)
			}
		})
	}
}

func TestMergeStaticRefusesAnInlineExperimental(t *testing.T) {
	if _, err := MergeStatic([]byte("experimental: {}\n"), BouncerAlias); err == nil {
		t.Fatal("an inline experimental mapping cannot be spliced")
	}
}

func TestStaticFromReusesAnExistingAlias(t *testing.T) {
	static := "experimental:\n  plugins:\n    bouncer:\n      moduleName: " + BouncerModule + "\n      version: v1.6.0\n"
	st := StaticFrom("/etc/traefik/traefik.yml", []byte(static))
	if !st.Ready {
		t.Fatalf("not ready: %s", st.Note)
	}
	if st.Alias != "bouncer" {
		t.Fatalf("alias = %q, want bouncer", st.Alias)
	}
	if st.Merged != "" {
		t.Fatalf("a second declaration was added:\n%s", st.Merged)
	}
}

func bouncerAnswers(t *testing.T, mode answers.Mode) *answers.Answers {
	t.Helper()
	a := answers.Defaults(mode)
	a.Domain = "example.com"
	a.Hosts.Manager = "manager.example.com"
	a.TLS.Email = "me@example.com"
	a.Mounts.StaticConfig = true
	a.Restart.Method = answers.RestartPoisonPill
	a.CrowdSec.Mode = answers.CrowdSecInstall
	a.CrowdSec.BouncerPlugin = true
	switch mode {
	case answers.ModeFullNative:
		a.Network.TraefikAPIPort = "8081"
	case answers.ModeTMNative:
		a.Config.Layout = answers.LayoutDirectory
	case answers.ModeAgentDocker, answers.ModeAgentBinary:
		a.Restart.Method = answers.RestartNone
	}
	a.Finalize()
	if err := a.Validate(); err != nil {
		t.Fatalf("%s: %v", mode, err)
	}
	if err := a.GenerateSecrets(); err != nil {
		t.Fatal(err)
	}
	a.SetSecret(answers.SecretTMAAPIKey, "tma-key")
	return a
}

func TestMiddlewarePluginKeyMatchesTheDeclaredAlias(t *testing.T) {
	existing := "experimental:\n  plugins:\n    bouncer:\n      moduleName: " + BouncerModule + "\n      version: v1.6.0\n"
	for _, mode := range answers.Modes {
		a := bouncerAnswers(t, mode)
		if !a.CrowdSec.BouncerPlugin {
			t.Fatalf("%s: the bouncer plugin should be available here", mode)
		}
		for _, reuse := range []bool{false, true} {
			var static Static
			if !a.Mode.HasTraefik() {
				body := "api:\n  dashboard: true\n"
				if reuse {
					body = existing
				}
				static = StaticFrom(a.Mounts.StaticConfigPath, []byte(body))
			} else if reuse {
				continue
			}
			plan := Bouncer(a, static)
			if !plan.Enabled {
				t.Fatalf("%s: the plugin was not installed: %s", mode, plan.Note)
			}
			wantAlias := BouncerAlias
			if reuse {
				wantAlias = "bouncer"
			}
			if plan.Alias != wantAlias {
				t.Fatalf("%s reuse=%v: alias = %q, want %q", mode, reuse, plan.Alias, wantAlias)
			}
			out, err := Render(Input{Answers: a, User: "alice", Static: static})
			if err != nil {
				t.Fatalf("%s: %v", mode, err)
			}
			declared := plan.Alias
			if _, written := findFile(out, plan.StaticPath); written {
				declared = declaredAlias(t, out, plan.StaticPath)
			} else if !reuse {
				t.Fatalf("%s: nothing declares the plugin in %s", mode, plan.StaticPath)
			}
			if declared != plan.Alias {
				t.Fatalf("%s reuse=%v: traefik.yml declares %q but the plan uses %q", mode, reuse, declared, plan.Alias)
			}
			if plan.MiddlewarePath == "" {
				continue
			}
			mw, ok := findFile(out, plan.MiddlewarePath)
			if !ok {
				t.Fatalf("%s: no middleware at %s", mode, plan.MiddlewarePath)
			}
			if !strings.Contains(mw.Content, "\n        "+declared+":\n") {
				t.Fatalf("%s reuse=%v: the middleware does not key its plugin block on %q:\n%s", mode, reuse, declared, mw.Content)
			}
		}
	}
}

func declaredAlias(t *testing.T, out *Output, path string) string {
	t.Helper()
	f, ok := findFile(out, path)
	if !ok {
		return ""
	}
	facts, err := ParseStatic([]byte(f.Content))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return facts.Alias
}

func TestBouncerLapiHostIsAlwaysExplicit(t *testing.T) {
	for _, mode := range answers.Modes {
		a := bouncerAnswers(t, mode)
		static := StaticFrom(a.Mounts.StaticConfigPath, []byte("api:\n  dashboard: true\n"))
		plan := Bouncer(a, static)
		want := "crowdsec:8080"
		if mode.IsSystemd() {
			want = "127.0.0.1:8080"
		}
		if plan.LapiHost != want {
			t.Errorf("%s: lapi host = %q, want %q", mode, plan.LapiHost, want)
		}
		if plan.LapiScheme != "http" {
			t.Errorf("%s: lapi scheme = %q", mode, plan.LapiScheme)
		}
	}
}

func TestLapiEndpoint(t *testing.T) {
	cases := map[string][2]string{
		"http://crowdsec:8080":              {"crowdsec:8080", "http"},
		"http://127.0.0.1:8080":             {"127.0.0.1:8080", "http"},
		"https://lapi.example.com":          {"lapi.example.com:8080", "https"},
		"https://lapi.example.com:9443/":    {"lapi.example.com:9443", "https"},
		"http://10.0.0.5:8080/v1/decisions": {"10.0.0.5:8080", "http"},
		"crowdsec.internal":                 {"crowdsec.internal:8080", "http"},
		"http://[fd00::1]:8080":             {"[fd00::1]:8080", "http"},
		"http://":                           {"", "http"},
	}
	for in, want := range cases {
		host, scheme := lapiEndpoint(in)
		if host != want[0] || scheme != want[1] {
			t.Errorf("lapiEndpoint(%q) = %q %q, want %q %q", in, host, scheme, want[0], want[1])
		}
	}
}

func TestBouncerMiddlewareOmitsAnEmptyTrustedIPList(t *testing.T) {
	a := bouncerAnswers(t, answers.ModeFull)
	a.Config.Layout = answers.LayoutDirectory
	a.Finalize()
	out, err := Render(Input{Answers: a, User: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	mw, ok := findFile(out, "traefik/config/"+BouncerFileName)
	if !ok {
		t.Fatal("no middleware file")
	}
	if strings.Contains(mw.Content, "ForwardedHeadersTrustedIPs") {
		t.Fatalf("an empty list was written:\n%s", mw.Content)
	}
	var doc any
	if err := yaml.Unmarshal([]byte(mw.Content), &doc); err != nil {
		t.Fatalf("not valid yaml: %v", err)
	}
}

func TestBouncerSeedsTrustedIPsIntoTheMiddleware(t *testing.T) {
	a := bouncerAnswers(t, answers.ModeTMDocker)
	a.Config.Layout = answers.LayoutDirectory
	a.Finalize()
	static := StaticFrom(a.Mounts.StaticConfigPath, []byte(`entryPoints:
  http:
    address: ":80"
  https:
    address: ":443"
    forwardedHeaders:
      trustedIPs:
        - 127.0.0.1/32
        - 172.17.0.0/16
        - 100.64.0.0/10
`))
	out, err := Render(Input{Answers: a, User: "alice", Static: static})
	if err != nil {
		t.Fatal(err)
	}
	mw, ok := findFile(out, "config/"+BouncerFileName)
	if !ok {
		t.Fatal("no middleware file")
	}
	want := "          ForwardedHeadersTrustedIPs:\n            - 127.0.0.1/32\n            - 172.17.0.0/16\n            - 100.64.0.0/10\n"
	if !strings.Contains(mw.Content, want) {
		t.Fatalf("trusted ips not seeded:\n%s", mw.Content)
	}
}

func TestBouncerNeverWritesTheMiddlewareIntoAPreExistingSingleFile(t *testing.T) {
	a := bouncerAnswers(t, answers.ModeTMNative)
	a.Config.Layout = answers.LayoutSingle
	a.Config.Path = "/etc/traefik/dynamic.yml"
	a.Finalize()
	static := StaticFrom(a.Mounts.StaticConfigPath, []byte("api:\n  dashboard: true\n"))
	plan := Bouncer(a, static)
	if !plan.Enabled {
		t.Fatalf("the plugin should still be declared: %s", plan.Note)
	}
	if plan.MiddlewarePath != "" {
		t.Fatalf("the middleware must not be merged into %s", plan.MiddlewarePath)
	}
	if plan.Note == "" {
		t.Fatal("no reason was given for skipping the middleware")
	}
	out, err := Render(Input{Answers: a, User: "alice", Static: static})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findFile(out, a.Config.Path); ok {
		t.Fatal("the user's dynamic.yml was written")
	}
	if _, ok := findFile(out, a.Mounts.StaticConfigPath); !ok {
		t.Fatal("the plugin declaration was not written")
	}
	if snippet := MiddlewareYAML(a, plan); !strings.Contains(snippet, "CrowdsecLapiHost: 127.0.0.1:8080") {
		t.Fatalf("the printable snippet is wrong:\n%s", snippet)
	}
}

func TestBouncerFillsOnlyIntoSomebodyElsesDirectory(t *testing.T) {
	owned := map[answers.Mode]bool{
		answers.ModeFull:               false,
		answers.ModeFullNative:         false,
		answers.ModeAgentDockerTraefik: false,
		answers.ModeTMDocker:           false,
		answers.ModeTMNative:           true,
		answers.ModeAgentDocker:        true,
		answers.ModeAgentBinary:        true,
	}
	for mode, wantFill := range owned {
		a := bouncerAnswers(t, mode)
		a.Config.Layout = answers.LayoutDirectory
		a.Finalize()
		static := StaticFrom(a.Mounts.StaticConfigPath, []byte("api:\n  dashboard: true\n"))
		plan := Bouncer(a, static)
		if plan.MiddlewarePath == "" {
			t.Fatalf("%s: no middleware path", mode)
		}
		if plan.Fill != wantFill {
			t.Errorf("%s: fill = %v, want %v", mode, plan.Fill, wantFill)
		}
	}
}

func TestBouncerIsOffWithoutAnswerOrStaticConfig(t *testing.T) {
	a := bouncerAnswers(t, answers.ModeTMDocker)
	a.CrowdSec.BouncerPlugin = false
	if p := Bouncer(a, Static{}); p.On || p.Enabled {
		t.Fatal("the plugin is off but the plan is on")
	}
	b := bouncerAnswers(t, answers.ModeTMDocker)
	p := Bouncer(b, Static{Note: "boom"})
	if p.Enabled {
		t.Fatal("the plugin cannot be declared without the static config")
	}
	if p.Note != "boom" {
		t.Fatalf("note = %q", p.Note)
	}
}

func TestIsPlaceholder(t *testing.T) {
	yes := []string{
		"",
		"# only a comment\n",
		"{}\n",
		"null\n",
		"---\n",
		"http:\n  routers: {}\n  services: {}\n  middlewares: {}\n",
		"# Traefik dynamic configuration\n# Traefik Manager writes routers, services and middlewares here.\n",
	}
	for _, s := range yes {
		if !IsPlaceholder([]byte(s)) {
			t.Errorf("%q should be an empty placeholder", s)
		}
	}
	no := []string{
		"http:\n  middlewares:\n    x:\n      plugin: {}\n",
		"http:\n  routers:\n    a:\n      rule: Host(`x`)\n",
		"a: [\n",
	}
	for _, s := range no {
		if IsPlaceholder([]byte(s)) {
			t.Errorf("%q is real content", s)
		}
	}
}
